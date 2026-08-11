using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace HydrusTagger.Core.Hydrus;

public static class HydrusServiceCollectionExtensions
{
    public static IServiceCollection AddHydrusClient(
        this IServiceCollection services,
        IConfiguration configuration)
    {
        services.Configure<HydrusClientOptions>(configuration.GetSection(HydrusClientOptions.SectionName));

        services.AddHttpClient<IHydrusClient, HydrusClient>(HydrusClient.HttpClientName, (sp, http) =>
            {
                var options = sp.GetRequiredService<IOptions<HydrusClientOptions>>().Value;
                options.Validate();

                // Address may or may not carry a trailing slash (the legacy
                // config.json had one). Normalize so relative paths resolve.
                http.BaseAddress = new Uri(options.Address.TrimEnd('/') + "/");
                http.Timeout = options.Timeout;
                http.DefaultRequestHeaders.Add(HydrusClient.AccessKeyHeader, options.ApiKey);
            })
            .AddHttpMessageHandler(sp =>
            {
                var options = sp.GetRequiredService<IOptions<HydrusClientOptions>>().Value;
                var logger = sp.GetRequiredService<ILoggerFactory>().CreateLogger<HydrusRetryHandler>();
                return new HydrusRetryHandler(options.MaxRetries, logger);
            });

        return services;
    }
}
