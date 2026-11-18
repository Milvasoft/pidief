using System.Globalization;
using System.IO.Compression;
using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Pidief.Client;

/// <summary>Typed client of the pidief API.</summary>
public interface IPidiefClient
{
    /// <summary>HTML (with assets such as css/site.css, img/logo.png) to PDF.</summary>
    Task<byte[]> HtmlToPdfAsync(string html, HtmlOptions? options = null, IEnumerable<PidiefFile>? assets = null, CancellationToken cancellationToken = default);
    /// <summary>Fills a Handlebars/Mustache template with data and renders it to PDF.</summary>
    Task<byte[]> TemplateToPdfAsync(string template, object? data, TemplateOptions? options = null, IEnumerable<PidiefFile>? assets = null, CancellationToken cancellationToken = default);
    /// <summary>Fills a template and returns the HTML (preview).</summary>
    Task<string> TemplatePreviewAsync(string template, object? data, TemplateOptions? options = null, CancellationToken cancellationToken = default);
    /// <summary>Photos and scans to one PDF.</summary>
    Task<byte[]> ImagesToPdfAsync(IEnumerable<PidiefFile> images, ImagesOptions? options = null, CancellationToken cancellationToken = default);
    /// <summary>Merges PDFs in order.</summary>
    Task<byte[]> MergeAsync(IEnumerable<PidiefFile> pdfs, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Splits a PDF into parts.</summary>
    Task<IReadOnlyList<PidiefFile>> SplitAsync(PidiefFile pdf, SplitOptions? options = null, CancellationToken cancellationToken = default);
    /// <summary>Keeps the selected pages in the given order.</summary>
    Task<byte[]> SelectPagesAsync(PidiefFile pdf, string pages, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Deletes the selected pages.</summary>
    Task<byte[]> DeletePagesAsync(PidiefFile pdf, string pages, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Rotates pages clockwise by a multiple of 90 degrees.</summary>
    Task<byte[]> RotatePagesAsync(PidiefFile pdf, int degrees, string? pages = null, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Adds a watermark (text, or the image when given).</summary>
    Task<byte[]> WatermarkAsync(PidiefFile pdf, StampOptions options, PidiefFile? image = null, CancellationToken cancellationToken = default);
    /// <summary>Adds a stamp (text, or the image when given).</summary>
    Task<byte[]> StampAsync(PidiefFile pdf, StampOptions options, PidiefFile? image = null, CancellationToken cancellationToken = default);
    /// <summary>Shrinks a PDF.</summary>
    Task<byte[]> CompressAsync(PidiefFile pdf, CompressOptions? options = null, CancellationToken cancellationToken = default);
    /// <summary>Converts to PDF/A-2b.</summary>
    Task<byte[]> ToPdfAAsync(PidiefFile pdf, MetadataOptions? metadata = null, CancellationToken cancellationToken = default);
    /// <summary>Protects a PDF with AES-256.</summary>
    Task<byte[]> EncryptAsync(PidiefFile pdf, EncryptOptions options, CancellationToken cancellationToken = default);
    /// <summary>Removes the password.</summary>
    Task<byte[]> DecryptAsync(PidiefFile pdf, string password, CancellationToken cancellationToken = default);
    /// <summary>Sets document information.</summary>
    Task<byte[]> SetMetadataAsync(PidiefFile pdf, MetadataOptions metadata, CancellationToken cancellationToken = default);
    /// <summary>Page count, sizes, metadata and fonts.</summary>
    Task<PdfInfo> GetInfoAsync(PidiefFile pdf, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Text of every page.</summary>
    Task<PdfText> GetTextAsync(PidiefFile pdf, string? password = null, CancellationToken cancellationToken = default);
    /// <summary>Renders pages to images (thumbnails, previews).</summary>
    Task<IReadOnlyList<PidiefFile>> RenderAsync(PidiefFile pdf, RenderOptions? options = null, CancellationToken cancellationToken = default);
    /// <summary>Starts any operation as a background job (path such as "/v1/html", options as for the typed method).</summary>
    Task<PidiefJob> StartJobAsync(string operationPath, IEnumerable<PidiefFile> files, object? options = null, CancellationToken cancellationToken = default);
    /// <summary>Job status.</summary>
    Task<PidiefJob> GetJobAsync(string jobId, CancellationToken cancellationToken = default);
    /// <summary>Waits for a job and returns its result files (a ZIP result is unpacked).</summary>
    Task<IReadOnlyList<PidiefFile>> WaitForJobAsync(string jobId, TimeSpan? pollInterval = null, CancellationToken cancellationToken = default);
    /// <summary>Whether the server is ready (Chromium available, not shutting down).</summary>
    Task<bool> IsReadyAsync(CancellationToken cancellationToken = default);
}

/// <summary>Error returned by pidief (RFC 9457 problem details).</summary>
public sealed class PidiefException : Exception
{
    /// <summary>Creates the exception.</summary>
    public PidiefException(HttpStatusCode statusCode, string message, string? requestId = null, Exception? inner = null)
        : base(message, inner)
    {
        StatusCode = statusCode;
        RequestId = requestId;
    }

    /// <summary>HTTP status.</summary>
    public HttpStatusCode StatusCode { get; }

    /// <summary>Server request id for log lookup.</summary>
    public string? RequestId { get; }

    /// <summary>True for 400: the input or the options are wrong, retrying will not help.</summary>
    public bool IsInvalidRequest => StatusCode == HttpStatusCode.BadRequest;
}

/// <summary>HTTP implementation of <see cref="IPidiefClient"/>.</summary>
public sealed class PidiefClient : IPidiefClient
{
    internal static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web)
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        // Send Turkish text as UTF-8 instead of \uXXXX escapes (the body is not embedded in HTML).
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    };

    private readonly HttpClient _http;
    private readonly PidiefClientOptions _options;

    /// <summary>Creates a client on an HttpClient (from IHttpClientFactory or your own).</summary>
    public PidiefClient(HttpClient http, PidiefClientOptions options)
    {
        _http = http ?? throw new ArgumentNullException(nameof(http));
        _options = options ?? throw new ArgumentNullException(nameof(options));
        if (_options.BaseAddress is not null)
        {
            _http.BaseAddress = _options.BaseAddress;
        }
        if (_http.BaseAddress is null)
        {
            throw new ArgumentException("PidiefClientOptions.BaseAddress is required.", nameof(options));
        }
        // Per-attempt timeouts are handled here, so the HttpClient must not cut retries short.
        _http.Timeout = System.Threading.Timeout.InfiniteTimeSpan;
    }

    /// <inheritdoc />
    public Task<byte[]> HtmlToPdfAsync(string html, HtmlOptions? options = null, IEnumerable<PidiefFile>? assets = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/html", Prepend(new PidiefFile("index.html", Encoding.UTF8.GetBytes(html), "text/html"), assets), options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> TemplateToPdfAsync(string template, object? data, TemplateOptions? options = null, IEnumerable<PidiefFile>? assets = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/template", TemplateFiles(template, data, assets), options, cancellationToken);

    /// <inheritdoc />
    public async Task<string> TemplatePreviewAsync(string template, object? data, TemplateOptions? options = null, CancellationToken cancellationToken = default)
    {
        var preview = options is null ? null : new { options.Strict, options.Empty, options.Timezone };
        var bytes = await BytesAsync("/v1/template/preview", TemplateFiles(template, data, null), preview, cancellationToken).ConfigureAwait(false);
        return Encoding.UTF8.GetString(bytes);
    }

    /// <inheritdoc />
    public Task<byte[]> ImagesToPdfAsync(IEnumerable<PidiefFile> images, ImagesOptions? options = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/images", images, options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> MergeAsync(IEnumerable<PidiefFile> pdfs, string? password = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/merge", pdfs, new { password }, cancellationToken);

    /// <inheritdoc />
    public async Task<IReadOnlyList<PidiefFile>> SplitAsync(PidiefFile pdf, SplitOptions? options = null, CancellationToken cancellationToken = default)
        => (await SendAsync("/v1/split", [pdf], options, cancellationToken).ConfigureAwait(false)).Files;

    /// <inheritdoc />
    public Task<byte[]> SelectPagesAsync(PidiefFile pdf, string pages, string? password = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/pages/select", [pdf], new { pages, password }, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> DeletePagesAsync(PidiefFile pdf, string pages, string? password = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/pages/delete", [pdf], new { pages, password }, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> RotatePagesAsync(PidiefFile pdf, int degrees, string? pages = null, string? password = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/pages/rotate", [pdf], new { degrees, pages, password }, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> WatermarkAsync(PidiefFile pdf, StampOptions options, PidiefFile? image = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/watermark", image is null ? [pdf] : [pdf, image], options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> StampAsync(PidiefFile pdf, StampOptions options, PidiefFile? image = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/stamp", image is null ? [pdf] : [pdf, image], options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> CompressAsync(PidiefFile pdf, CompressOptions? options = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/compress", [pdf], options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> ToPdfAAsync(PidiefFile pdf, MetadataOptions? metadata = null, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/pdfa", [pdf], metadata is null ? null : new { metadata.Title, metadata.Author, metadata.Subject, metadata.Keywords, metadata.Password }, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> EncryptAsync(PidiefFile pdf, EncryptOptions options, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/encrypt", [pdf], options, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> DecryptAsync(PidiefFile pdf, string password, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/decrypt", [pdf], new { password }, cancellationToken);

    /// <inheritdoc />
    public Task<byte[]> SetMetadataAsync(PidiefFile pdf, MetadataOptions metadata, CancellationToken cancellationToken = default)
        => BytesAsync("/v1/metadata", [pdf], metadata, cancellationToken);

    /// <inheritdoc />
    public Task<PdfInfo> GetInfoAsync(PidiefFile pdf, string? password = null, CancellationToken cancellationToken = default)
        => JsonAsync<PdfInfo>("/v1/info", pdf, password, cancellationToken);

    /// <inheritdoc />
    public Task<PdfText> GetTextAsync(PidiefFile pdf, string? password = null, CancellationToken cancellationToken = default)
        => JsonAsync<PdfText>("/v1/text", pdf, password, cancellationToken);

    /// <inheritdoc />
    public async Task<IReadOnlyList<PidiefFile>> RenderAsync(PidiefFile pdf, RenderOptions? options = null, CancellationToken cancellationToken = default)
        => (await SendAsync("/v1/render", [pdf], options, cancellationToken).ConfigureAwait(false)).Files;

    /// <inheritdoc />
    public async Task<PidiefJob> StartJobAsync(string operationPath, IEnumerable<PidiefFile> files, object? options = null, CancellationToken cancellationToken = default)
    {
        var res = await SendAsync(operationPath.TrimEnd('/') + "?async=true", files, options, cancellationToken).ConfigureAwait(false);
        return JsonSerializer.Deserialize<PidiefJob>(res.Body, Json) ?? throw new PidiefException(res.Status, "empty job response");
    }

    /// <inheritdoc />
    public async Task<PidiefJob> GetJobAsync(string jobId, CancellationToken cancellationToken = default)
    {
        var res = await ExecuteAsync(() => new HttpRequestMessage(HttpMethod.Get, "v1/jobs/" + Uri.EscapeDataString(jobId)), cancellationToken).ConfigureAwait(false);
        return JsonSerializer.Deserialize<PidiefJob>(res.Body, Json) ?? throw new PidiefException(res.Status, "empty job response");
    }

    /// <inheritdoc />
    public async Task<IReadOnlyList<PidiefFile>> WaitForJobAsync(string jobId, TimeSpan? pollInterval = null, CancellationToken cancellationToken = default)
    {
        var delay = pollInterval ?? TimeSpan.FromMilliseconds(500);
        while (true)
        {
            var job = await GetJobAsync(jobId, cancellationToken).ConfigureAwait(false);
            if (job.State == "failed")
            {
                var status = job.ErrorKind switch
                {
                    "invalid" => HttpStatusCode.BadRequest,
                    "timeout" => HttpStatusCode.GatewayTimeout,
                    _ => HttpStatusCode.InternalServerError,
                };
                throw new PidiefException(status, job.Error ?? "job failed");
            }
            if (job.State == "succeeded")
            {
                var res = await ExecuteAsync(() => new HttpRequestMessage(HttpMethod.Get, $"v1/jobs/{Uri.EscapeDataString(jobId)}/result"), cancellationToken).ConfigureAwait(false);
                return res.Files;
            }
            await Task.Delay(delay, cancellationToken).ConfigureAwait(false);
        }
    }

    /// <inheritdoc />
    public async Task<bool> IsReadyAsync(CancellationToken cancellationToken = default)
    {
        try
        {
            using var cts = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
            cts.CancelAfter(_options.Timeout);
            using var res = await _http.GetAsync("health/ready", cts.Token).ConfigureAwait(false);
            return res.IsSuccessStatusCode;
        }
        catch (Exception e) when (e is HttpRequestException or OperationCanceledException && !cancellationToken.IsCancellationRequested)
        {
            return false;
        }
    }

    // ---------- transport ----------

    private static IEnumerable<PidiefFile> Prepend(PidiefFile first, IEnumerable<PidiefFile>? rest)
        => rest is null ? [first] : [first, .. rest];

    private static IEnumerable<PidiefFile> TemplateFiles(string template, object? data, IEnumerable<PidiefFile>? assets)
    {
        var json = data switch
        {
            null => "{}",
            string s => s,
            JsonElement el => el.GetRawText(),
            _ => JsonSerializer.Serialize(data, Json),
        };
        var files = new List<PidiefFile>
        {
            new("template.html", Encoding.UTF8.GetBytes(template), "text/html"),
            new("data.json", Encoding.UTF8.GetBytes(json), "application/json"),
        };
        if (assets is not null)
        {
            files.AddRange(assets);
        }
        return files;
    }

    private async Task<byte[]> BytesAsync(string path, IEnumerable<PidiefFile> files, object? options, CancellationToken ct)
    {
        var res = await SendAsync(path, files, options, ct).ConfigureAwait(false);
        return res.Body;
    }

    private async Task<T> JsonAsync<T>(string path, PidiefFile pdf, string? password, CancellationToken ct)
    {
        var res = await SendAsync(path, [pdf], new { password }, ct).ConfigureAwait(false);
        return JsonSerializer.Deserialize<T>(res.Body, Json) ?? throw new PidiefException(res.Status, "empty response");
    }

    private Task<Response> SendAsync(string path, IEnumerable<PidiefFile> files, object? options, CancellationToken ct)
    {
        var list = files.ToList();
        var optionsJson = options is null ? null : JsonSerializer.Serialize(options, options.GetType(), Json);
        return ExecuteAsync(() =>
        {
            var content = new MultipartFormDataContent();
            foreach (var f in list)
            {
                var part = new ByteArrayContent(f.Content);
                part.Headers.ContentType = new MediaTypeHeaderValue(f.ContentType ?? "application/octet-stream");
                // Keep relative paths (css/site.css); non-ASCII names (sözleşme.pdf) go in filename* (RFC 5987).
                var name = f.Name.Replace("\\", "/").Replace("\"", "");
                var ascii = name.All(c => c >= 32 && c < 127);
                var disposition = new ContentDispositionHeaderValue("form-data")
                {
                    Name = "\"files\"",
                    FileName = "\"" + (ascii ? name : new string(name.Select(c => c >= 32 && c < 127 ? c : '_').ToArray())) + "\"",
                };
                if (!ascii)
                {
                    disposition.FileNameStar = name;
                }
                part.Headers.ContentDisposition = disposition;
                content.Add(part);
            }
            if (optionsJson is not null && optionsJson != "{}")
            {
                content.Add(new StringContent(optionsJson, Encoding.UTF8), "options");
            }
            return new HttpRequestMessage(HttpMethod.Post, path.TrimStart('/')) { Content = content };
        }, ct);
    }

    internal sealed record Response(HttpStatusCode Status, byte[] Body, string? ContentType, IReadOnlyList<PidiefFile> Files);

    private static bool IsTransient(HttpStatusCode s)
        => s is HttpStatusCode.TooManyRequests or HttpStatusCode.BadGateway or HttpStatusCode.ServiceUnavailable or HttpStatusCode.GatewayTimeout;

    private async Task<Response> ExecuteAsync(Func<HttpRequestMessage> build, CancellationToken ct)
    {
        for (var attempt = 0; ; attempt++)
        {
            using var request = build();
            if (!string.IsNullOrEmpty(_options.ApiKey))
            {
                request.Headers.TryAddWithoutValidation("X-Api-Key", _options.ApiKey);
            }
            using var attemptCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            attemptCts.CancelAfter(_options.Timeout);
            HttpResponseMessage? res = null;
            Exception? failure = null;
            try
            {
                res = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, attemptCts.Token).ConfigureAwait(false);
            }
            catch (HttpRequestException e)
            {
                failure = e;
            }
            catch (OperationCanceledException e) when (!ct.IsCancellationRequested)
            {
                failure = new TimeoutException($"pidief did not answer within {_options.Timeout}.", e);
            }

            using (res)
            {
                if (res is not null && res.IsSuccessStatusCode)
                {
                    var body = await res.Content.ReadAsByteArrayAsync(attemptCts.Token).ConfigureAwait(false);
                    var type = res.Content.Headers.ContentType?.MediaType;
                    return new Response(res.StatusCode, body, type, ToFiles(res, body, type));
                }
                var retryable = failure is not null || (res is not null && IsTransient(res.StatusCode));
                if (!retryable || attempt >= _options.MaxRetries)
                {
                    if (res is null)
                    {
                        throw new PidiefException(0, failure!.Message, null, failure);
                    }
                    throw await ToExceptionAsync(res, ct).ConfigureAwait(false);
                }
                await Task.Delay(RetryDelay(attempt, res), ct).ConfigureAwait(false);
            }
        }
    }

    internal TimeSpan RetryDelay(int attempt, HttpResponseMessage? res)
    {
        if (res?.Headers.RetryAfter is { } ra)
        {
            var d = ra.Delta ?? (ra.Date is { } date ? date - DateTimeOffset.UtcNow : (TimeSpan?)null);
            if (d is { } delta && delta > TimeSpan.Zero)
            {
                return delta < _options.RetryMaxDelay ? delta : _options.RetryMaxDelay;
            }
        }
        var baseMs = _options.RetryBaseDelay.TotalMilliseconds * Math.Pow(2, attempt);
        var jitter = Random.Shared.NextDouble() * baseMs * 0.25;
        return TimeSpan.FromMilliseconds(Math.Min(baseMs + jitter, _options.RetryMaxDelay.TotalMilliseconds));
    }

    private static async Task<PidiefException> ToExceptionAsync(HttpResponseMessage res, CancellationToken ct)
    {
        string? detail = null, requestId = null;
        try
        {
            var body = await res.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
            using var doc = JsonDocument.Parse(body);
            detail = doc.RootElement.TryGetProperty("detail", out var d) ? d.GetString() : body;
            requestId = doc.RootElement.TryGetProperty("requestId", out var r) ? r.GetString() : null;
        }
        catch (JsonException)
        {
        }
        requestId ??= res.Headers.TryGetValues("X-Request-Id", out var ids) ? ids.FirstOrDefault() : null;
        var message = string.Create(CultureInfo.InvariantCulture, $"pidief {(int)res.StatusCode} {res.ReasonPhrase}: {detail ?? "(no detail)"}");
        return new PidiefException(res.StatusCode, message, requestId);
    }

    private static IReadOnlyList<PidiefFile> ToFiles(HttpResponseMessage res, byte[] body, string? type)
    {
        if (type == "application/zip")
        {
            using var zip = new ZipArchive(new MemoryStream(body), ZipArchiveMode.Read);
            var files = new List<PidiefFile>();
            foreach (var e in zip.Entries)
            {
                using var s = e.Open();
                using var ms = new MemoryStream();
                s.CopyTo(ms);
                files.Add(new PidiefFile(e.FullName, ms.ToArray(), ContentTypeFor(e.FullName)));
            }
            return files;
        }
        var name = res.Content.Headers.ContentDisposition?.FileNameStar ?? res.Content.Headers.ContentDisposition?.FileName?.Trim('"') ?? "result";
        return [new PidiefFile(name, body, type)];
    }

    private static string ContentTypeFor(string name) => Path.GetExtension(name).ToLowerInvariant() switch
    {
        ".pdf" => "application/pdf",
        ".png" => "image/png",
        ".jpg" or ".jpeg" => "image/jpeg",
        ".webp" => "image/webp",
        _ => "application/octet-stream",
    };
}

/// <summary>Writes TimeSpan as a Go duration string ("1.5s", "500ms").</summary>
public sealed class GoDurationConverter : JsonConverter<TimeSpan?>
{
    /// <inheritdoc />
    public override TimeSpan? Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
        => throw new NotSupportedException();

    /// <inheritdoc />
    public override void Write(Utf8JsonWriter writer, TimeSpan? value, JsonSerializerOptions options)
    {
        if (value is null)
        {
            writer.WriteNullValue();
            return;
        }
        writer.WriteStringValue(((long)value.Value.TotalMilliseconds).ToString(CultureInfo.InvariantCulture) + "ms");
    }
}
