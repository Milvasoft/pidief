package server

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/milvasoft/pidief/internal/ops"
)

type obj = map[string]any

var (
	openapiOnce sync.Once
	openapiDoc  []byte
)

// OpenAPI returns the OpenAPI 3.1 document generated from the ops registry.
func OpenAPI() []byte {
	openapiOnce.Do(func() { openapiDoc, _ = json.MarshalIndent(buildOpenAPI(), "", "  ") })
	return openapiDoc
}

func schemaFor(f ops.Field) obj {
	s := obj{}
	switch f.Kind {
	case reflect.String:
		s["type"] = "string"
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.Int:
		s["type"] = "integer"
	case reflect.Float64:
		s["type"] = "number"
	case reflect.Slice:
		item := "string"
		if f.Elem == reflect.Int {
			item = "integer"
		}
		s["type"] = "array"
		s["items"] = obj{"type": item}
	case reflect.Map:
		s["type"] = "object"
		s["additionalProperties"] = obj{"type": "string"}
	}
	if f.Help != "" {
		s["description"] = f.Help
	}
	if len(f.Enum) > 0 {
		s["enum"] = f.Enum
	}
	if f.Default != "" {
		switch f.Kind {
		case reflect.Bool:
			s["default"] = f.Default == "true"
		case reflect.Int:
			n, _ := strconv.Atoi(f.Default)
			s["default"] = n
		case reflect.Float64:
			n, _ := strconv.ParseFloat(f.Default, 64)
			s["default"] = n
		default:
			s["default"] = f.Default
		}
	}
	return s
}

func problemResp(desc string) obj {
	return obj{"description": desc, "content": obj{"application/problem+json": obj{"schema": obj{"$ref": "#/components/schemas/Problem"}}}}
}

func buildOpenAPI() obj {
	paths := obj{}
	for _, op := range ops.All() {
		fields := ops.Fields(op.Params())
		params := obj{}
		for _, f := range fields {
			params[f.Name] = schemaFor(f)
		}
		multipart := obj{
			"files": obj{"type": "array", "items": obj{"type": "string", "format": "binary"},
				"description": op.Inputs.Description + ". Send each file as a part with a filename; the field name is free."},
			"options": obj{"type": "string", "description": "all parameters as one JSON object (alternative to separate fields)"},
		}
		jsonBody := obj{
			"files": obj{"type": "array", "description": op.Inputs.Description, "items": obj{"$ref": "#/components/schemas/InputFile"}},
		}
		for _, ti := range op.TextInputs {
			multipart[ti.Field] = obj{"type": "string", "description": ti.Help}
			jsonBody[ti.Field] = obj{"type": "string", "description": ti.Help}
		}
		for k, v := range params {
			multipart[k] = v
			jsonBody[k] = v
		}
		if _, ok := jsonBody["data"]; ok {
			jsonBody["data"] = obj{"description": "template data: a JSON object, or the same as a string", "oneOf": []any{obj{"type": "object"}, obj{"type": "string"}}}
		}
		var ok200 obj
		switch op.Output {
		case "pdf":
			ok200 = obj{"description": "the PDF", "content": obj{"application/pdf": obj{"schema": obj{"type": "string", "format": "binary"}}}}
		case "html":
			ok200 = obj{"description": "the filled HTML", "content": obj{"text/html": obj{"schema": obj{"type": "string"}}}}
		case "json":
			ref := "#/components/schemas/Info"
			if op.Path == "/v1/text" {
				ref = "#/components/schemas/Text"
			}
			ok200 = obj{"description": "JSON result", "content": obj{"application/json": obj{"schema": obj{"$ref": ref}}}}
		case "pdfs":
			ok200 = obj{"description": "a ZIP with the parts", "content": obj{"application/zip": obj{"schema": obj{"type": "string", "format": "binary"}}}}
		case "images":
			ok200 = obj{"description": "one page: the image; more pages: a ZIP", "content": obj{
				"image/png": obj{"schema": obj{"type": "string", "format": "binary"}}, "image/jpeg": obj{"schema": obj{"type": "string", "format": "binary"}},
				"image/webp": obj{"schema": obj{"type": "string", "format": "binary"}}, "application/zip": obj{"schema": obj{"type": "string", "format": "binary"}},
			}}
		}
		opID := strings.ReplaceAll(strings.Title(strings.ReplaceAll(op.Name, " ", " ")), " ", "") //nolint:staticcheck
		paths[op.Path] = obj{"post": obj{
			"operationId": opID,
			"summary":     op.Summary,
			"description": op.Summary + "\n\nCLI: `pidief " + op.Name + " [files...] [--flags]`. Inputs: " + op.Inputs.Description +
				". Add `?async=true` (or `Prefer: respond-async`) to get a job id instead of waiting.",
			"tags": []string{tag(op)},
			"parameters": []any{
				obj{"name": "async", "in": "query", "schema": obj{"type": "boolean"}, "description": "run as a background job; returns 202 with the job"},
			},
			"requestBody": obj{"required": true, "content": obj{
				"multipart/form-data": obj{"schema": obj{"type": "object", "properties": multipart}},
				"application/json":    obj{"schema": obj{"type": "object", "properties": jsonBody}},
				"application/pdf":     obj{"schema": obj{"type": "string", "format": "binary"}, "description": "raw single input; parameters in the query string"},
			}},
			"responses": obj{
				"200": ok200,
				"202": obj{"description": "job accepted", "content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/Job"}}}},
				"400": problemResp("invalid input or parameters"),
				"401": problemResp("missing or invalid API key"),
				"413": problemResp("request too large"),
				"429": problemResp("queue full"),
				"503": problemResp("no free slot in time"),
				"504": problemResp("operation timed out"),
				"500": problemResp("server error"),
			},
		}}
	}
	jobPath := func(summary string, ok obj, method string) obj {
		return obj{method: obj{
			"summary": summary, "tags": []string{"jobs"},
			"parameters": []any{obj{"name": "id", "in": "path", "required": true, "schema": obj{"type": "string"}}},
			"responses":  obj{"200": ok, "204": obj{"description": "deleted"}, "404": problemResp("unknown or expired job"), "409": problemResp("job not finished")},
		}}
	}
	paths["/v1/jobs/{id}"] = merge(
		jobPath("Job status", obj{"description": "job", "content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/Job"}}}}, "get"),
		jobPath("Cancel/delete a job", obj{"description": "deleted"}, "delete"))
	paths["/v1/jobs/{id}/result"] = jobPath("Job result (same body as the synchronous call)", obj{"description": "result"}, "get")
	paths["/health/live"] = obj{"get": obj{"summary": "Liveness", "tags": []string{"health"}, "security": []any{}, "responses": obj{"200": obj{"description": "alive"}}}}
	paths["/health/ready"] = obj{"get": obj{"summary": "Readiness (Chromium can start, not shutting down)", "tags": []string{"health"}, "security": []any{}, "responses": obj{"200": obj{"description": "ready"}, "503": problemResp("not ready")}}}
	paths["/metrics"] = obj{"get": obj{"summary": "Prometheus metrics", "tags": []string{"health"}, "security": []any{}, "responses": obj{"200": obj{"description": "metrics", "content": obj{"text/plain": obj{}}}}}}

	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": "pidief", "version": Version,
			"description": "PDF toolkit by Milvasoft: HTML/template to PDF, images to PDF, merge, split, pages, watermark, stamp, compress, PDF/A-2b, encryption, metadata, render, text.\n\nApache-2.0. Source: https://github.com/Milvasoft/pidief",
			"license":     obj{"name": "Apache-2.0", "identifier": "Apache-2.0"}},
		"servers":  []any{obj{"url": "/"}},
		"security": []any{obj{"apiKey": []any{}}},
		"paths":    paths,
		"components": obj{
			"securitySchemes": obj{"apiKey": obj{"type": "apiKey", "in": "header", "name": "X-Api-Key"}},
			"schemas": obj{
				"InputFile": obj{"type": "object", "required": []string{"name", "content"}, "properties": obj{
					"name":    obj{"type": "string", "description": "file name, may contain a relative path (img/logo.png)"},
					"content": obj{"type": "string", "contentEncoding": "base64"},
				}},
				"Problem": obj{"type": "object", "properties": obj{
					"title": obj{"type": "string"}, "status": obj{"type": "integer"}, "detail": obj{"type": "string"}, "requestId": obj{"type": "string"},
				}},
				"Job": obj{"type": "object", "properties": obj{
					"id": obj{"type": "string"}, "operation": obj{"type": "string"},
					"state":     obj{"type": "string", "enum": []string{JobQueued, JobRunning, JobSucceeded, JobFailed}},
					"createdAt": obj{"type": "string", "format": "date-time"}, "startedAt": obj{"type": "string", "format": "date-time"},
					"finishedAt": obj{"type": "string", "format": "date-time"}, "expiresAt": obj{"type": "string", "format": "date-time"},
					"error": obj{"type": "string"}, "errorKind": obj{"type": "string", "enum": []string{"invalid", "timeout", "server"}},
					"resultUrl": obj{"type": "string"},
				}},
				"Text": obj{"type": "object", "properties": obj{"pages": obj{"type": "array", "items": obj{"type": "object", "properties": obj{
					"number": obj{"type": "integer"}, "text": obj{"type": "string"},
				}}}}},
				"Info": obj{"type": "object", "properties": obj{
					"pageCount": obj{"type": "integer"}, "version": obj{"type": "string"}, "encrypted": obj{"type": "boolean"},
					"tagged": obj{"type": "boolean"}, "linearized": obj{"type": "boolean"}, "fileSize": obj{"type": "integer"},
					"pdfa": obj{"type": "string", "description": "declared PDF/A part and conformance, e.g. 2B"},
					"metadata": obj{"type": "object", "properties": obj{
						"title": obj{"type": "string"}, "author": obj{"type": "string"}, "subject": obj{"type": "string"}, "keywords": obj{"type": "string"},
						"creator": obj{"type": "string"}, "producer": obj{"type": "string"}, "creationDate": obj{"type": "string"}, "modDate": obj{"type": "string"},
					}},
					"properties": obj{"type": "object", "additionalProperties": obj{"type": "string"}},
					"pages": obj{"type": "array", "items": obj{"type": "object", "properties": obj{
						"number": obj{"type": "integer"}, "width": obj{"type": "number", "description": "points"}, "height": obj{"type": "number"}, "rotation": obj{"type": "integer"},
					}}},
					"fonts": obj{"type": "array", "items": obj{"type": "object", "properties": obj{
						"name": obj{"type": "string"}, "type": obj{"type": "string"}, "embedded": obj{"type": "boolean"}, "encoding": obj{"type": "string"},
					}}},
				}},
			},
		},
	}
}

func tag(op *ops.Op) string {
	switch op.Path {
	case "/v1/html", "/v1/template", "/v1/template/preview", "/v1/images":
		return "create"
	case "/v1/info", "/v1/text", "/v1/render":
		return "read"
	}
	return "edit"
}

func merge(a, b obj) obj {
	for k, v := range b {
		a[k] = v
	}
	return a
}
