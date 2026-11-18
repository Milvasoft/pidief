using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Options;

namespace Pidief.Client;

/// <summary>Dependency injection registration.</summary>
public static class ServiceCollectionExtensions
{
    /// <summary>Registers <see cref="IPidiefClient"/> on a typed HttpClient from IHttpClientFactory.</summary>
    /// <example>
    /// <code>
    /// services.AddPidiefClient(o =>
    /// {
    ///     o.BaseAddress = new Uri(configuration["Pidief:Url"]!);
    ///     o.ApiKey = configuration["Pidief:ApiKey"];
    /// });
    /// </code>
    /// </example>
    public static IHttpClientBuilder AddPidiefClient(this IServiceCollection services, Action<PidiefClientOptions> configure)
    {
        ArgumentNullException.ThrowIfNull(services);
        ArgumentNullException.ThrowIfNull(configure);
        services.Configure(configure);
        return services.AddHttpClient<IPidiefClient, PidiefClient>((http, sp) =>
            new PidiefClient(http, sp.GetRequiredService<IOptions<PidiefClientOptions>>().Value));
    }
}
