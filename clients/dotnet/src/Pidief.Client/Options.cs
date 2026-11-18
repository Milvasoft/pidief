using System.Text.Json.Serialization;

namespace Pidief.Client;

/// <summary>Connection settings of <see cref="PidiefClient"/>.</summary>
public sealed class PidiefClientOptions
{
    /// <summary>Base address of the pidief API, e.g. http://pidief:3000/.</summary>
    public Uri? BaseAddress { get; set; }

    /// <summary>Value sent in the X-Api-Key header.</summary>
    public string? ApiKey { get; set; }

    /// <summary>Timeout of one HTTP attempt (default 120 s). The server also caps the work with the timeout parameter.</summary>
    public TimeSpan Timeout { get; set; } = TimeSpan.FromSeconds(120);

    /// <summary>Retries after the first attempt for 429, 502, 503, 504, connection errors and attempt timeouts (default 3).</summary>
    public int MaxRetries { get; set; } = 3;

    /// <summary>First retry delay; doubled on every retry with jitter. Retry-After from the server wins (default 500 ms).</summary>
    public TimeSpan RetryBaseDelay { get; set; } = TimeSpan.FromMilliseconds(500);

    /// <summary>Upper bound of one retry delay (default 10 s).</summary>
    public TimeSpan RetryMaxDelay { get; set; } = TimeSpan.FromSeconds(10);
}

/// <summary>An input or output file. <see cref="Name"/> may contain a relative path (img/logo.png).</summary>
/// <param name="Name">File name.</param>
/// <param name="Content">File bytes.</param>
/// <param name="ContentType">Media type, if known.</param>
public sealed record PidiefFile(string Name, byte[] Content, string? ContentType = null)
{
    /// <summary>Reads a file from disk, keeping only its file name.</summary>
    public static async Task<PidiefFile> FromPathAsync(string path, CancellationToken cancellationToken = default)
        => new(Path.GetFileName(path), await File.ReadAllBytesAsync(path, cancellationToken).ConfigureAwait(false));
}

/// <summary>Options of HTML and template rendering. Unset values use the server defaults.</summary>
public class HtmlOptions
{
    /// <summary>A0-A6, B4, B5, Letter, Legal, Tabloid or WxH with a unit such as 210x297mm (default A4).</summary>
    public string? Paper { get; set; }
    /// <summary>Landscape orientation.</summary>
    public bool? Landscape { get; set; }
    /// <summary>All margins, e.g. 15mm (default 10mm).</summary>
    public string? Margin { get; set; }
    /// <summary>Top margin.</summary>
    public string? MarginTop { get; set; }
    /// <summary>Right margin.</summary>
    public string? MarginRight { get; set; }
    /// <summary>Bottom margin.</summary>
    public string? MarginBottom { get; set; }
    /// <summary>Left margin.</summary>
    public string? MarginLeft { get; set; }
    /// <summary>Header HTML; spans with class pageNumber, totalPages, title, date are filled in. Use inline styles and a font-size.</summary>
    public string? Header { get; set; }
    /// <summary>Footer HTML.</summary>
    public string? Footer { get; set; }
    /// <summary>Adds a centred "Sayfa X / Y" footer when no footer is given.</summary>
    public bool? PageNumbers { get; set; }
    /// <summary>Text of the page number footer, {page} and {pages} are replaced (default "Sayfa {page} / {pages}").</summary>
    public string? PageNumberText { get; set; }
    /// <summary>Print background colours and images (default true).</summary>
    public bool? PrintBackground { get; set; }
    /// <summary>Use the CSS @page size instead of <see cref="Paper"/>.</summary>
    [JsonPropertyName("preferCssPageSize")]
    public bool? PreferCssPageSize { get; set; }
    /// <summary>Rendering scale 0.1-2.</summary>
    public double? Scale { get; set; }
    /// <summary>Pages to print, e.g. 1-3,5.</summary>
    public string? PageRanges { get; set; }
    /// <summary>print or screen.</summary>
    public string? Media { get; set; }
    /// <summary>Wait until this CSS selector exists.</summary>
    public string? WaitForSelector { get; set; }
    /// <summary>Wait until this JavaScript expression is truthy.</summary>
    public string? WaitForExpression { get; set; }
    /// <summary>Extra wait before printing.</summary>
    [JsonConverter(typeof(GoDurationConverter))]
    public TimeSpan? WaitDelay { get; set; }
    /// <summary>Wait until the network is idle.</summary>
    public bool? WaitNetworkIdle { get; set; }
    /// <summary>Allow internet resources (the server may forbid it).</summary>
    public bool? AllowExternal { get; set; }
    /// <summary>Fail when a referenced resource is missing or blocked.</summary>
    public bool? FailOnResourceError { get; set; }
    /// <summary>Make the embedded Onest font available as font-family Onest (default true).</summary>
    public bool? Onest { get; set; }
    /// <summary>Tagged (accessible) PDF.</summary>
    public bool? Tagged { get; set; }
    /// <summary>Bookmarks from headings.</summary>
    public bool? Outline { get; set; }
    /// <summary>Conversion timeout on the server (default 30 s, capped by the server).</summary>
    [JsonConverter(typeof(GoDurationConverter))]
    public TimeSpan? Timeout { get; set; }
    /// <summary>Document title (default: the HTML title).</summary>
    public string? Title { get; set; }
    /// <summary>Document author.</summary>
    public string? Author { get; set; }
    /// <summary>Document subject.</summary>
    public string? Subject { get; set; }
    /// <summary>Document keywords.</summary>
    public string? Keywords { get; set; }
    /// <summary>Convert the result to PDF/A-2b.</summary>
    [JsonPropertyName("pdfa")]
    public bool? PdfA { get; set; }
}

/// <summary>Options of template rendering.</summary>
public class TemplateOptions : HtmlOptions
{
    /// <summary>Fail when the template uses a value the data does not have.</summary>
    public bool? Strict { get; set; }
    /// <summary>Text printed for an empty value, e.g. "-".</summary>
    public string? Empty { get; set; }
    /// <summary>Time zone for dates (default Europe/Istanbul).</summary>
    public string? Timezone { get; set; }
}

/// <summary>Options of images to PDF.</summary>
public sealed class ImagesOptions
{
    /// <summary>Page size, or "fit" for the image size (default A4).</summary>
    public string? Paper { get; set; }
    /// <summary>auto, portrait or landscape (default auto: follows each image).</summary>
    public string? Orientation { get; set; }
    /// <summary>Page margin (default 0mm).</summary>
    public string? Margin { get; set; }
    /// <summary>Downsample above this resolution (default 200; 0 keeps originals).</summary>
    public int? MaxDpi { get; set; }
    /// <summary>JPEG quality of re-encoded images (default 80).</summary>
    public int? Quality { get; set; }
    /// <summary>Convert to grayscale.</summary>
    public bool? Grayscale { get; set; }
    /// <summary>Document title.</summary>
    public string? Title { get; set; }
}

/// <summary>Options of a watermark or stamp.</summary>
public sealed class StampOptions
{
    /// <summary>Text; %p is the page number and %P the page count. Leave empty to use an image.</summary>
    public string? Text { get; set; }
    /// <summary>Page selection, e.g. 1-3,even (default all).</summary>
    public string? Pages { get; set; }
    /// <summary>Font size in points.</summary>
    public int? FontSize { get; set; }
    /// <summary>Size relative to the page width (0-1).</summary>
    public double? Scale { get; set; }
    /// <summary>Opacity (0-1].</summary>
    public double? Opacity { get; set; }
    /// <summary>Rotation in degrees.</summary>
    public double? Rotation { get; set; }
    /// <summary>c, tl, tc, tr, l, r, bl, bc, br.</summary>
    public string? Position { get; set; }
    /// <summary>Horizontal offset in points.</summary>
    public double? OffsetX { get; set; }
    /// <summary>Vertical offset in points.</summary>
    public double? OffsetY { get; set; }
    /// <summary>Text colour #rrggbb.</summary>
    public string? Color { get; set; }
    /// <summary>Put it behind the page content.</summary>
    public bool? Behind { get; set; }
    /// <summary>Password of an encrypted input.</summary>
    public string? Password { get; set; }
}

/// <summary>Options of split. Use one of Span, At or Ranges (default: one page per file).</summary>
public sealed class SplitOptions
{
    /// <summary>Every N pages.</summary>
    public int? Span { get; set; }
    /// <summary>Start a new part at these pages.</summary>
    public IReadOnlyList<int>? At { get; set; }
    /// <summary>One part per range, e.g. "1-3".</summary>
    public IReadOnlyList<string>? Ranges { get; set; }
    /// <summary>Password of an encrypted input.</summary>
    public string? Password { get; set; }
}

/// <summary>Options of compress.</summary>
public sealed class CompressOptions
{
    /// <summary>JPEG quality of recompressed images (default 70; 0 keeps images).</summary>
    public int? ImageQuality { get; set; }
    /// <summary>Downscale images whose longest side is larger (default 2000).</summary>
    public int? MaxImagePixels { get; set; }
    /// <summary>Convert recompressed images to grayscale.</summary>
    public bool? Grayscale { get; set; }
    /// <summary>Password of an encrypted input.</summary>
    public string? Password { get; set; }
}

/// <summary>Options of encrypt.</summary>
public sealed class EncryptOptions
{
    /// <summary>Password to open the document.</summary>
    public string? UserPassword { get; set; }
    /// <summary>Password to change permissions (default: the user password).</summary>
    public string? OwnerPassword { get; set; }
    /// <summary>Allow printing (default true).</summary>
    public bool? AllowPrint { get; set; }
    /// <summary>Allow copying.</summary>
    public bool? AllowCopy { get; set; }
    /// <summary>Allow changes.</summary>
    public bool? AllowModify { get; set; }
    /// <summary>Allow comments and forms.</summary>
    public bool? AllowAnnotate { get; set; }
}

/// <summary>Document information to set.</summary>
public sealed class MetadataOptions
{
    /// <summary>Title.</summary>
    public string? Title { get; set; }
    /// <summary>Author.</summary>
    public string? Author { get; set; }
    /// <summary>Subject.</summary>
    public string? Subject { get; set; }
    /// <summary>Keywords.</summary>
    public string? Keywords { get; set; }
    /// <summary>Creating application.</summary>
    public string? Creator { get; set; }
    /// <summary>Custom properties.</summary>
    public IDictionary<string, string>? Properties { get; set; }
    /// <summary>Password of an encrypted input.</summary>
    public string? Password { get; set; }
}

/// <summary>Options of render (PDF pages to images).</summary>
public sealed class RenderOptions
{
    /// <summary>Page selection (default all).</summary>
    public string? Pages { get; set; }
    /// <summary>Resolution (default 96, max 600).</summary>
    public int? Dpi { get; set; }
    /// <summary>png, jpeg or webp (default png).</summary>
    public string? Format { get; set; }
    /// <summary>jpeg/webp quality.</summary>
    public int? Quality { get; set; }
    /// <summary>Fit into this width (thumbnails).</summary>
    public int? MaxWidth { get; set; }
    /// <summary>Fit into this height.</summary>
    public int? MaxHeight { get; set; }
    /// <summary>Password of an encrypted input.</summary>
    public string? Password { get; set; }
}

/// <summary>Document information returned by <see cref="IPidiefClient.GetInfoAsync"/>.</summary>
public sealed class PdfInfo
{
    /// <summary>Number of pages.</summary>
    public int PageCount { get; set; }
    /// <summary>PDF version.</summary>
    public string? Version { get; set; }
    /// <summary>Whether the file is encrypted.</summary>
    public bool Encrypted { get; set; }
    /// <summary>Whether the file is tagged.</summary>
    public bool Tagged { get; set; }
    /// <summary>Whether the file is linearized.</summary>
    public bool Linearized { get; set; }
    /// <summary>File size in bytes.</summary>
    public long FileSize { get; set; }
    /// <summary>Declared PDF/A level, e.g. 2B.</summary>
    [JsonPropertyName("pdfa")]
    public string? PdfA { get; set; }
    /// <summary>Document information.</summary>
    public PdfMetadata Metadata { get; set; } = new();
    /// <summary>Custom properties.</summary>
    public Dictionary<string, string>? Properties { get; set; }
    /// <summary>Pages.</summary>
    public List<PdfPage> Pages { get; set; } = [];
    /// <summary>Fonts.</summary>
    public List<PdfFont> Fonts { get; set; } = [];
}

/// <summary>Document information fields.</summary>
public sealed class PdfMetadata
{
    /// <summary>Title.</summary>
    public string? Title { get; set; }
    /// <summary>Author.</summary>
    public string? Author { get; set; }
    /// <summary>Subject.</summary>
    public string? Subject { get; set; }
    /// <summary>Keywords.</summary>
    public string? Keywords { get; set; }
    /// <summary>Creator.</summary>
    public string? Creator { get; set; }
    /// <summary>Producer.</summary>
    public string? Producer { get; set; }
    /// <summary>Creation date (PDF date string).</summary>
    public string? CreationDate { get; set; }
    /// <summary>Modification date (PDF date string).</summary>
    public string? ModDate { get; set; }
}

/// <summary>A page, in points.</summary>
public sealed class PdfPage
{
    /// <summary>1-based number.</summary>
    public int Number { get; set; }
    /// <summary>Width in points.</summary>
    public double Width { get; set; }
    /// <summary>Height in points.</summary>
    public double Height { get; set; }
    /// <summary>Rotation in degrees.</summary>
    public int Rotation { get; set; }
}

/// <summary>A font resource.</summary>
public sealed class PdfFont
{
    /// <summary>Base font name.</summary>
    public string? Name { get; set; }
    /// <summary>Font type.</summary>
    public string? Type { get; set; }
    /// <summary>Whether the font program is embedded.</summary>
    public bool Embedded { get; set; }
}

/// <summary>Text of a document.</summary>
public sealed class PdfText
{
    /// <summary>Pages.</summary>
    public List<PdfPageText> Pages { get; set; } = [];
}

/// <summary>Text of one page.</summary>
public sealed class PdfPageText
{
    /// <summary>1-based number.</summary>
    public int Number { get; set; }
    /// <summary>Text, lines separated by \n.</summary>
    public string Text { get; set; } = "";
}

/// <summary>A background job.</summary>
public sealed class PidiefJob
{
    /// <summary>Job id.</summary>
    public string Id { get; set; } = "";
    /// <summary>API path of the operation.</summary>
    public string Operation { get; set; } = "";
    /// <summary>queued, running, succeeded or failed.</summary>
    public string State { get; set; } = "";
    /// <summary>Creation time.</summary>
    public DateTimeOffset CreatedAt { get; set; }
    /// <summary>Start time.</summary>
    public DateTimeOffset? StartedAt { get; set; }
    /// <summary>Finish time.</summary>
    public DateTimeOffset? FinishedAt { get; set; }
    /// <summary>Until when the result is kept.</summary>
    public DateTimeOffset? ExpiresAt { get; set; }
    /// <summary>Error message of a failed job.</summary>
    public string? Error { get; set; }
    /// <summary>invalid, timeout or server.</summary>
    public string? ErrorKind { get; set; }
    /// <summary>Whether the job has finished.</summary>
    public bool IsFinished => State is "succeeded" or "failed";
}
