using HydrusTagger.Core.Tagging;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.DependencyInjection.Extensions;

namespace HydrusTagger.Taggers.Vrchat;

public static class VrchatServiceCollectionExtensions
{
    public static IServiceCollection AddVrchatTagger(
        this IServiceCollection services, IConfiguration configuration)
    {
        services.Configure<VrchatTaggerOptions>(configuration.GetSection(VrchatTaggerOptions.SectionName));
        services.TryAddSingleton<IVrchatChunkStore, EfVrchatChunkStore>();
        services.AddTagger<VrchatTagger>();

        return services;
    }
}
