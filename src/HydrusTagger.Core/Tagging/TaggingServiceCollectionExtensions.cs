using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.DependencyInjection.Extensions;

namespace HydrusTagger.Core.Tagging;

public static class TaggingServiceCollectionExtensions
{
    /// <summary>
    /// Register the host. Taggers are registered separately with
    /// <see cref="AddTagger{T}"/>; the host receives all of them.
    /// </summary>
    public static IServiceCollection AddTaggerHost(this IServiceCollection services)
    {
        services.TryAddSingleton<ITaggerStateStore, InMemoryTaggerStateStore>();
        services.AddSingleton<TaggerHost>();
        return services;
    }

    /// <summary>
    /// Register a tagger under every role it implements, backed by one instance,
    /// so <c>IEnumerable&lt;ITagger&gt;</c> and a direct
    /// <c>IFileExtractor</c> resolve to the same object and share its state.
    /// </summary>
    /// <remarks>
    /// The concrete registration is <c>TryAdd</c>, so a tagger that needs
    /// construction arguments can be registered by hand first and still be
    /// wired into all of its roles here.
    /// </remarks>
    public static IServiceCollection AddTagger<T>(this IServiceCollection services)
        where T : class, ITagger
    {
        services.TryAddSingleton<T>();
        services.AddSingleton<ITagger>(sp => sp.GetRequiredService<T>());

        if (typeof(IFileExtractor).IsAssignableFrom(typeof(T)))
        {
            services.AddSingleton(sp => (IFileExtractor)sp.GetRequiredService<T>());
        }

        if (typeof(IFileTagger).IsAssignableFrom(typeof(T)))
        {
            services.AddSingleton(sp => (IFileTagger)sp.GetRequiredService<T>());
        }

        if (typeof(ICorpusTagger).IsAssignableFrom(typeof(T)))
        {
            services.AddSingleton(sp => (ICorpusTagger)sp.GetRequiredService<T>());
        }

        return services;
    }
}
