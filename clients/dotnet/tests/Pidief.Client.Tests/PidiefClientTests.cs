using System.Net;
using System.Net.Http.Headers;
using System.Text;
using Microsoft.Extensions.DependencyInjection;
using Xunit;

namespace Pidief.Client.Tests;

/// <summary>Unit tests with a fake handler.</summary>
public sealed class PidiefClientTests
{
    private sealed class FakeHandler(Func<HttpRequestMessage, int, CancellationToken, Task<HttpResponseMessage>> respond) : HttpMessageHandler
    {
        public int Calls;
        public List<string> Bodies { get; } = [];
        public List<HttpRequestMessage> Requests { get; } = [];

        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Requests.Add(request);
            Bodies.Add(request.Content is null ? "" : await request.Content.ReadAsStringAsync(cancellationToken));
            return await respond(request, Interlocked.Increment(ref Calls), cancellationToken);
        }
    }

    private static readonly byte[] Pdf = Encoding.ASCII.GetBytes("%PDF-1.7\n%%EOF");

    private static HttpResponseMessage PdfResponse()
    {
        var res = new HttpResponseMessage(HttpStatusCode.OK) { Content = new ByteArrayContent(Pdf) };
        res.Content.Headers.ContentType = new MediaTypeHeaderValue("application/pdf");
        return res;
    }

    private static HttpResponseMessage Problem(HttpStatusCode status, string detail)
    {
        var res = new HttpResponseMessage(status)
        {
            Content = new StringContent($$"""{"title":"x","status":{{(int)status}},"detail":"{{detail}}","requestId":"req-1"}""", Encoding.UTF8, "application/problem+json"),
        };
        return res;
    }

    private static (PidiefClient Client, FakeHandler Handler) Create(Func<HttpRequestMessage, int, Task<HttpResponseMessage>> respond, Action<PidiefClientOptions>? configure = null)
        => Create((r, n, _) => respond(r, n), configure);

    private static (PidiefClient Client, FakeHandler Handler) Create(Func<HttpRequestMessage, int, CancellationToken, Task<HttpResponseMessage>> respond, Action<PidiefClientOptions>? configure = null)
    {
        var handler = new FakeHandler(respond);
        var options = new PidiefClientOptions
        {
            BaseAddress = new Uri("http://pidief.test/"),
            ApiKey = "anahtar",
            RetryBaseDelay = TimeSpan.FromMilliseconds(1),
            RetryMaxDelay = TimeSpan.FromMilliseconds(5),
        };
        configure?.Invoke(options);
        return (new PidiefClient(new HttpClient(handler), options), handler);
    }

    [Fact]
    public async Task HtmlToPdf_SendsMultipartWithOptionsAndApiKey()
    {
        var (client, handler) = Create((_, _) => Task.FromResult(PdfResponse()));
        var pdf = await client.HtmlToPdfAsync("<h1>Şantiye</h1>",
            new HtmlOptions { PageNumbers = true, PdfA = true, WaitDelay = TimeSpan.FromMilliseconds(250), PreferCssPageSize = true },
            [new PidiefFile("img/logo.png", [1, 2, 3]), new PidiefFile("sözleşme ekleri.css", [4])]);

        Assert.Equal(Pdf, pdf);
        var req = handler.Requests.Single();
        Assert.Equal("http://pidief.test/v1/html", req.RequestUri!.ToString());
        Assert.Equal("anahtar", req.Headers.GetValues("X-Api-Key").Single());
        var body = handler.Bodies.Single();
        Assert.Contains("filename=\"index.html\"", body);
        Assert.Contains("filename=\"img/logo.png\"", body);
        Assert.Contains("filename*=utf-8''s%C3%B6zle%C5%9Fme%20ekleri.css", body, StringComparison.OrdinalIgnoreCase);
        Assert.Contains("\"pageNumbers\":true", body);
        Assert.Contains("\"pdfa\":true", body);
        Assert.Contains("\"preferCssPageSize\":true", body);
        Assert.Contains("\"waitDelay\":\"250ms\"", body);
        Assert.DoesNotContain("\"landscape\"", body); // unset values are not sent
        Assert.Contains("<h1>Şantiye</h1>", body);
    }

    [Fact]
    public async Task Template_SendsDataAsJsonFile()
    {
        var (client, handler) = Create((_, _) => Task.FromResult(PdfResponse()));
        await client.TemplateToPdfAsync("<p>{{firma}}</p>", new { firma = "Örnek Yapı", tutar = 1500.5m }, new TemplateOptions { Strict = true });
        var body = handler.Bodies.Single();
        Assert.Contains("filename=\"template.html\"", body);
        Assert.Contains("filename=\"data.json\"", body);
        Assert.Contains("\"firma\":\"Örnek Yapı\"", body);
        Assert.Contains("\"tutar\":1500.5", body);
        Assert.Contains("\"strict\":true", body);
    }

    [Fact]
    public async Task RetriesTransientErrorsHonouringRetryAfter()
    {
        var (client, handler) = Create((_, n) =>
        {
            if (n == 1)
            {
                var busy = Problem(HttpStatusCode.TooManyRequests, "queue full");
                busy.Headers.RetryAfter = new RetryConditionHeaderValue(TimeSpan.FromMilliseconds(1));
                return Task.FromResult(busy);
            }
            if (n == 2)
            {
                throw new HttpRequestException("connection reset");
            }
            return Task.FromResult(PdfResponse());
        });
        var pdf = await client.MergeAsync([new PidiefFile("a.pdf", Pdf), new PidiefFile("b.pdf", Pdf)]);
        Assert.Equal(Pdf, pdf);
        Assert.Equal(3, handler.Calls);
        // Each attempt sends a full body again.
        Assert.All(handler.Bodies.Where(b => b.Length > 0), b => Assert.Contains("filename=\"b.pdf\"", b));
    }

    [Fact]
    public async Task DoesNotRetryBadRequestAndMapsProblem()
    {
        var (client, handler) = Create((_, _) => Task.FromResult(Problem(HttpStatusCode.BadRequest, "page selection x is invalid")));
        var ex = await Assert.ThrowsAsync<PidiefException>(() => client.SelectPagesAsync(new PidiefFile("a.pdf", Pdf), "x"));
        Assert.Equal(1, handler.Calls);
        Assert.True(ex.IsInvalidRequest);
        Assert.Equal("req-1", ex.RequestId);
        Assert.Contains("page selection", ex.Message);
    }

    [Fact]
    public async Task GivesUpAfterMaxRetries()
    {
        var (client, handler) = Create((_, _) => Task.FromResult(Problem(HttpStatusCode.ServiceUnavailable, "busy")), o => o.MaxRetries = 2);
        var ex = await Assert.ThrowsAsync<PidiefException>(() => client.CompressAsync(new PidiefFile("a.pdf", Pdf)));
        Assert.Equal(HttpStatusCode.ServiceUnavailable, ex.StatusCode);
        Assert.Equal(3, handler.Calls);
    }

    [Fact]
    public async Task AttemptTimeoutIsRetriedThenReported()
    {
        var (client, handler) = Create(async (req, _, ct) =>
        {
            await Task.Delay(TimeSpan.FromSeconds(5), ct);
            return PdfResponse();
        }, o => { o.Timeout = TimeSpan.FromMilliseconds(50); o.MaxRetries = 1; });
        var ex = await Assert.ThrowsAsync<PidiefException>(() => client.DecryptAsync(new PidiefFile("a.pdf", Pdf), "x"));
        Assert.IsType<TimeoutException>(ex.InnerException);
        Assert.Equal(2, handler.Calls);
    }

    [Fact]
    public async Task CallerCancellationIsNotRetried()
    {
        using var cts = new CancellationTokenSource(TimeSpan.FromMilliseconds(50));
        var (client, handler) = Create(async (_, _, ct) =>
        {
            await Task.Delay(TimeSpan.FromSeconds(5), ct);
            return PdfResponse();
        });
        await Assert.ThrowsAnyAsync<OperationCanceledException>(() => client.DecryptAsync(new PidiefFile("a.pdf", Pdf), "x", cts.Token));
        Assert.Equal(1, handler.Calls);
    }

    [Fact]
    public async Task SplitUnpacksZip()
    {
        using var ms = new MemoryStream();
        using (var zip = new System.IO.Compression.ZipArchive(ms, System.IO.Compression.ZipArchiveMode.Create, true))
        {
            foreach (var n in new[] { "part-001_1-1.pdf", "part-002_2-2.pdf" })
            {
                using var s = zip.CreateEntry(n).Open();
                s.Write(Pdf);
            }
        }
        var (client, _) = Create((_, _) =>
        {
            var res = new HttpResponseMessage(HttpStatusCode.OK) { Content = new ByteArrayContent(ms.ToArray()) };
            res.Content.Headers.ContentType = new MediaTypeHeaderValue("application/zip");
            return Task.FromResult(res);
        });
        var parts = await client.SplitAsync(new PidiefFile("a.pdf", Pdf), new SplitOptions { Span = 1 });
        Assert.Equal(["part-001_1-1.pdf", "part-002_2-2.pdf"], parts.Select(p => p.Name));
        Assert.All(parts, p => Assert.Equal("application/pdf", p.ContentType));
    }

    [Fact]
    public void RegistersWithDependencyInjection()
    {
        var services = new ServiceCollection();
        services.AddPidiefClient(o => { o.BaseAddress = new Uri("http://pidief:3000/"); o.ApiKey = "k"; });
        using var sp = services.BuildServiceProvider();
        Assert.IsType<PidiefClient>(sp.GetRequiredService<IPidiefClient>());
    }
}
