using HydrusTagger.Core.Tagging;
using HydrusTagger.Tests.Tagging.Fakes;
using Microsoft.Extensions.DependencyInjection;

namespace HydrusTagger.Tests.Tagging;

public class TaggingRegistrationTests
{
    private static ServiceProvider BuildProvider() =>
        new ServiceCollection()
            .AddSingleton(new FakeExtractorTagger("extract"))
            .AddSingleton(new FakeCorpusTagger("corpus"))
            .AddTagger<FakeExtractorTagger>()
            .AddTagger<FakeCorpusTagger>()
            .BuildServiceProvider();

    [Fact]
    public void EveryRegisteredTaggerIsVisibleToTheHost()
    {
        using var provider = BuildProvider();

        Assert.Equal(
            ["corpus", "extract"],
            provider.GetServices<ITagger>().Select(t => t.Id).Order(StringComparer.Ordinal));
    }

    [Fact]
    public void ATaggerRegisteredUnderSeveralRolesIsStillOneInstance()
    {
        // Registering the concrete type once per interface would hand the host
        // a different object than the one holding the extract stage's state.
        using var provider = BuildProvider();

        var concrete = provider.GetRequiredService<FakeExtractorTagger>();

        Assert.Same(concrete, provider.GetRequiredService<IFileExtractor>());
        Assert.Same(concrete, provider.GetRequiredService<IFileTagger>());
        Assert.Same(concrete, provider.GetServices<ITagger>().Single(t => t.Id == "extract"));
    }

    [Fact]
    public void ATaggerIsOnlyExposedUnderTheRolesItActuallyImplements()
    {
        using var provider = BuildProvider();

        Assert.Same(
            provider.GetRequiredService<FakeCorpusTagger>(),
            provider.GetRequiredService<ICorpusTagger>());

        // The corpus tagger is not an IFileTagger, so only the extractor should
        // answer that role.
        Assert.Single(provider.GetServices<IFileTagger>());
        Assert.Single(provider.GetServices<ICorpusTagger>());
    }

    [Fact]
    public void AddTaggerHostResolvesAHostOverAllRegisteredTaggers()
    {
        using var provider = new ServiceCollection()
            .AddLogging()
            .AddSingleton(new FakeExtractorTagger("extract"))
            .AddSingleton(new FakeFileTagger("plain"))
            .AddTagger<FakeExtractorTagger>()
            .AddTagger<FakeFileTagger>()
            .AddSingleton<Core.Hydrus.IHydrusClient>(new FakeHydrusClient())
            .AddTaggerHost()
            .BuildServiceProvider();

        var host = provider.GetRequiredService<TaggerHost>();

        Assert.Equal(["extract", "plain"], host.OrderedTaggers.Select(t => t.Id));
    }
}
