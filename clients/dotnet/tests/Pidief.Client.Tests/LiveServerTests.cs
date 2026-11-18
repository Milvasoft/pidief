using System.Text;
using Xunit;

namespace Pidief.Client.Tests;

/// <summary>End-to-end tests against a running pidief (set PIDIEF_URL, and PIDIEF_API_KEY when the server needs one).</summary>
public sealed class LiveServerTests
{
    private static readonly string? Url = Environment.GetEnvironmentVariable("PIDIEF_URL");

    private static PidiefClient Client() => new(new HttpClient(), new PidiefClientOptions
    {
        BaseAddress = new Uri(Url!),
        ApiKey = Environment.GetEnvironmentVariable("PIDIEF_API_KEY"),
        Timeout = TimeSpan.FromSeconds(60),
    });

    [Fact]
    public async Task EndToEnd()
    {
        if (string.IsNullOrEmpty(Url))
        {
            return; // no server configured
        }
        var client = Client();
        Assert.True(await client.IsReadyAsync());

        var html = "<html><head><title>Hak Ediş</title></head><body style=\"font-family:Onest\"><h1>Çağrı Şükrü İğdır ığüşöç</h1>"
                   + string.Concat(Enumerable.Range(1, 80).Select(i => $"<p>Satır {i}</p>")) + "</body></html>";
        var pdf = await client.HtmlToPdfAsync(html, new HtmlOptions { PageNumbers = true, PdfA = true, Author = "Milvasoft" });
        var file = new PidiefFile("hakedis.pdf", pdf);

        var info = await client.GetInfoAsync(file);
        Assert.True(info.PageCount >= 2);
        Assert.Equal("2B", info.PdfA);
        Assert.Equal("Hak Ediş", info.Metadata.Title);
        Assert.All(info.Fonts, f => Assert.True(f.Embedded, f.Name));
        Assert.Contains(info.Fonts, f => f.Name!.Contains("Onest"));

        var text = await client.GetTextAsync(file);
        Assert.Contains("Çağrı Şükrü İğdır ığüşöç", text.Pages[0].Text);
        Assert.Contains($"Sayfa 2 / {info.PageCount}", text.Pages[1].Text);

        var preview = await client.TemplatePreviewAsync("<p>{{firma}} {{para tutar}} {{tarih t \"d MMMM yyyy\"}}</p>",
            new { firma = "Örnek", tutar = 1234.5m, t = "2026-11-18" });
        Assert.Equal("<p>Örnek ₺1.234,50 18 Kasım 2026</p>", preview);

        var thumbs = await client.RenderAsync(file, new RenderOptions { Pages = "1-2", Format = "webp", MaxWidth = 160 });
        Assert.Equal(2, thumbs.Count);
        Assert.All(thumbs, t => Assert.Equal("image/webp", t.ContentType));

        var encrypted = await client.EncryptAsync(file, new EncryptOptions { UserPassword = "Gizli-1" });
        var bad = await Assert.ThrowsAsync<PidiefException>(() => client.GetInfoAsync(new PidiefFile("e.pdf", encrypted)));
        Assert.True(bad.IsInvalidRequest);
        var plain = await client.DecryptAsync(new PidiefFile("e.pdf", encrypted), "Gizli-1");
        Assert.Equal(info.PageCount, (await client.GetInfoAsync(new PidiefFile("p.pdf", plain))).PageCount);

        var job = await client.StartJobAsync("/v1/pages/select", [file], new { pages = "1,1" });
        var result = await client.WaitForJobAsync(job.Id, TimeSpan.FromMilliseconds(100));
        Assert.Equal(2, (await client.GetInfoAsync(result[0])).PageCount);

        var stamped = await client.StampAsync(file, new StampOptions { Text = "ONAYLI – İmza", Position = "br", FontSize = 14 });
        Assert.Contains("ONAYLI – İmza", (await client.GetTextAsync(new PidiefFile("s.pdf", stamped))).Pages[0].Text);
    }
}
