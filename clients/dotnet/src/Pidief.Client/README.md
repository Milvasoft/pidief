# Pidief.Client

Typed .NET client for the pidief PDF API (Apache-2.0, https://github.com/Milvasoft/pidief).

```csharp
services.AddPidiefClient(o =>
{
    o.BaseAddress = new Uri("http://pidief:3000/");
    o.ApiKey = configuration["Pidief:ApiKey"];
    o.Timeout = TimeSpan.FromSeconds(60);   // per attempt
    o.MaxRetries = 3;                        // 429/502/503/504, network errors, attempt timeouts
});

public sealed class ContractPdf(IPidiefClient pidief)
{
    public Task<byte[]> RenderAsync(string html, CancellationToken ct) =>
        pidief.HtmlToPdfAsync(html, new HtmlOptions { PageNumbers = true, Title = "Sözleşme", PdfA = true }, cancellationToken: ct);
}
```

Errors throw `PidiefException` (`StatusCode`, `RequestId`, `IsInvalidRequest`).
