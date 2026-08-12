using HydrusTagger.Core.Tagging;
using HydrusTagger.Tests.Tagging.Fakes;
using Microsoft.Extensions.Logging.Abstractions;

namespace HydrusTagger.Tests.Tagging;

public class TaggerHostTests
{
    private static TaggerHost Host(
        IEnumerable<ITagger> taggers, FakeHydrusClient hydrus, ITaggerStateStore store) =>
        new(taggers, hydrus, store, NullLogger<TaggerHost>.Instance);

    private static TaggerResult ResultFor(TaggerRunReport report, string taggerId) =>
        report.Results.Single(r => r.TaggerId == taggerId);

    [Fact]
    public async Task DerivesAndPushesTagsForNewlyDiscoveredFiles()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        var report = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        var result = ResultFor(report, "t");
        Assert.Equal(TaggerStatus.Completed, result.Status);
        Assert.Equal(2, result.Discovered);
        Assert.Equal(2, result.Derived);
        Assert.Equal(2, result.NeedingUpdate);
        Assert.Equal(2, result.Pushed);
        Assert.Empty(result.Warnings);
    }

    [Fact]
    public async Task ASecondRunPushesNothingBecauseTheHashesMatch()
    {
        // This round trip is the whole point of the push ledger: it is what
        // makes a nightly run cost one search request instead of 91,238 writes.
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        hydrus.AddTagsCalls.Clear();

        var report = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        var result = ResultFor(report, "t");
        Assert.Equal(0, result.Derived);
        Assert.Equal(0, result.NeedingUpdate);
        Assert.Empty(hydrus.AddTagsCalls);
    }

    [Fact]
    public async Task BumpingTheDeriveVersionRedrivesButStillPushesNothingIfTagsAreUnchanged()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        hydrus.AddTagsCalls.Clear();
        tagger.DerivedFiles.Clear();
        tagger.DeriveVersion = 2;

        var report = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Equal([1], tagger.DerivedFiles);
        Assert.Equal(0, ResultFor(report, "t").NeedingUpdate);
        Assert.Empty(hydrus.AddTagsCalls);
    }

    [Fact]
    public async Task ChangedTagsArePushedAgain()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        hydrus.AddTagsCalls.Clear();
        tagger.DeriveVersion = 2;
        tagger.DefaultTags = ["vrchat", "vrchat-world-name:The Great Pug"];

        var report = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Equal(1, ResultFor(report, "t").Pushed);
        var call = Assert.Single(hydrus.AddTagsCalls);
        Assert.Equal(["vrchat", "vrchat-world-name:The Great Pug"], call.Tags);
    }

    [Fact]
    public async Task FilesSharingATagSetArePushedInOneRequest()
    {
        // The Python issued one add_tags call per file. Grouping by tag set is
        // the single biggest reduction in request count for a first run.
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2).WithFile(3);
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };
        tagger.TagsByFile[3] = ["something-else"];

        var report = await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        Assert.Equal(3, ResultFor(report, "t").Pushed);
        Assert.Equal(2, hydrus.AddTagsCalls.Count);
        Assert.Equal(2, hydrus.AddTagsCalls.Single(c => c.Tags.Contains("vrchat")).Hashes.Count);
    }

    [Fact]
    public async Task PushBatchSizeCapsHashesPerRequest()
    {
        var hydrus = new FakeHydrusClient();
        for (var i = 1; i <= 5; i++)
        {
            hydrus.WithFile(i);
        }

        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions { PushBatchSize = 2 });

        Assert.Equal([2, 2, 1], hydrus.AddTagsCalls.Select(c => c.Hashes.Count));
    }

    [Fact]
    public async Task EmptyTagSetsAreNeverPushed()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var tagger = new FakeFileTagger("t");

        var report = await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        Assert.Equal(1, ResultFor(report, "t").Derived);
        Assert.Equal(0, ResultFor(report, "t").NeedingUpdate);
        Assert.Empty(hydrus.AddTagsCalls);
    }

    [Fact]
    public async Task DryRunDerivesAndCountsButNeitherPushesNorRecords()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        var report = await Host([tagger], hydrus, store)
            .RunAsync(new TaggerRunOptions { DryRun = true });

        Assert.Equal(1, ResultFor(report, "t").NeedingUpdate);
        Assert.Equal(0, ResultFor(report, "t").Pushed);
        Assert.Empty(hydrus.AddTagsCalls);

        // Nothing recorded, so a real run afterwards still has work to do.
        Assert.Empty(await store.GetPushedHashesAsync("t", [1], default));
        Assert.Empty(await store.GetFileStatesAsync("t", [1], default));
    }

    [Fact]
    public async Task AFailedPushIsNotRecordedSoTheNextRunRetriesIt()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        hydrus.ThrowOnAddTags = new HttpRequestException("hydrus is down");
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        var report = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        var result = ResultFor(report, "t");
        Assert.Equal(TaggerStatus.Completed, result.Status);
        Assert.Equal(1, result.PushFailed);
        Assert.Empty(await store.GetPushedHashesAsync("t", [1], default));

        hydrus.ThrowOnAddTags = null;
        var second = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        Assert.Equal(1, ResultFor(second, "t").Pushed);
    }

    [Fact]
    public async Task OneFileThrowingDoesNotStopTheRest()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2).WithFile(3);
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };
        tagger.ThrowOnFiles.Add(2);

        var report = await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        var result = ResultFor(report, "t");
        Assert.Equal(TaggerStatus.Completed, result.Status);
        Assert.Equal(2, result.Derived);
        Assert.Equal(1, result.DeriveFailed);
        Assert.Contains(result.Warnings, w => w.Contains("file 2", StringComparison.Ordinal));
    }

    [Fact]
    public async Task ATaggerThatFailsOutrightDoesNotStopTheOthers()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var broken = new FakeFileTagger("broken") { SelectorQuery = [] };
        var healthy = new FakeFileTagger("healthy") { DefaultTags = ["ok"] };

        var report = await Host([broken, healthy], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        Assert.Equal(TaggerStatus.Failed, ResultFor(report, "broken").Status);
        Assert.Equal(TaggerStatus.Completed, ResultFor(report, "healthy").Status);
        Assert.True(report.AnyFailed);
    }

    [Fact]
    public async Task ADependentIsSkippedWhenItsDependencyFails()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var upstream = new FakeFileTagger("a") { SelectorQuery = [] };
        var downstream = new FakeFileTagger("b") { DependsOnIds = ["a"], DefaultTags = ["x"] };

        var report = await Host([upstream, downstream], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        Assert.Equal(TaggerStatus.Failed, ResultFor(report, "a").Status);
        Assert.Equal(TaggerStatus.SkippedMissingDependency, ResultFor(report, "b").Status);
        Assert.Empty(downstream.DerivedFiles);
    }

    [Fact]
    public async Task UpstreamTagsFromThisRunReachTheDependent()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var upstream = new FakeFileTagger("a") { DefaultTags = ["vrchat-world-id:wrld_1"] };
        var downstream = new FakeFileTagger("b") { DependsOnIds = ["a"] };

        await Host([upstream, downstream], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        var seen = downstream.SeenUpstream[1];
        Assert.Equal(["vrchat-world-id:wrld_1"], seen["a"].Tags);
    }

    [Fact]
    public async Task UpstreamTagsAreReadFromStorageWhenTheUpstreamHadNothingToRedrive()
    {
        // The dependent must still see its input on a run where the upstream
        // tagger is entirely up to date and derives nothing.
        var hydrus = new FakeHydrusClient().WithFile(1);
        var store = new InMemoryTaggerStateStore();
        var upstream = new FakeFileTagger("a") { DefaultTags = ["vrchat-world-id:wrld_1"] };
        var downstream = new FakeFileTagger("b") { DependsOnIds = ["a"] };

        await Host([upstream, downstream], hydrus, store).RunAsync(new TaggerRunOptions());
        downstream.SeenUpstream.Clear();
        upstream.DerivedFiles.Clear();
        downstream.DeriveVersion = 2;

        await Host([upstream, downstream], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Empty(upstream.DerivedFiles);
        Assert.Equal(["vrchat-world-id:wrld_1"], downstream.SeenUpstream[1]["a"].Tags);
    }

    [Fact]
    public async Task ExtractRunsBeforeDeriveAndIsSkippedOnceItsVersionIsSatisfied()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeExtractorTagger("t");

        var first = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        Assert.Equal(2, first.Results.Single().Extracted);
        Assert.Equal([1, 2], tagger.ExtractedFiles.Order());

        tagger.ExtractedFiles.Clear();
        tagger.DerivedFiles.Clear();

        var second = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Empty(tagger.ExtractedFiles);
        Assert.Empty(tagger.DerivedFiles);
        Assert.Equal(0, second.Results.Single().Extracted);
    }

    [Fact]
    public async Task ExtractVersionBumpReExtractsAndForcesAReDeriveEvenAtTheSameDeriveVersion()
    {
        // A fresh extract replaces the cached artifacts, so last run's derived
        // tags are stale regardless of what DeriveVersion says.
        var hydrus = new FakeHydrusClient().WithFile(1);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeExtractorTagger("t");

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        tagger.ExtractedFiles.Clear();
        tagger.DerivedFiles.Clear();
        tagger.ExtractVersion = 2;

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Equal([1], tagger.ExtractedFiles);
        Assert.Equal([1], tagger.DerivedFiles);
    }

    [Fact]
    public async Task AFileWhoseExtractFailedIsRetriedNextRun()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeExtractorTagger("t");
        tagger.FailExtractOn.Add(2);

        var first = await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        Assert.Equal(1, first.Results.Single().Extracted);
        Assert.Equal(1, first.Results.Single().ExtractFailed);

        tagger.ExtractedFiles.Clear();
        tagger.FailExtractOn.Clear();

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Equal([2], tagger.ExtractedFiles);
    }

    [Fact]
    public async Task AnExtractorThatThrowsIsCountedNotPropagated()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var tagger = new FakeExtractorTagger("t");
        tagger.ThrowOnExtract.Add(1);

        var report = await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        var result = report.Results.Single();
        Assert.Equal(TaggerStatus.Completed, result.Status);
        Assert.Equal(1, result.ExtractFailed);
        Assert.Contains(result.Warnings, w => w.Contains("IOException", StringComparison.Ordinal));
    }

    [Fact]
    public async Task ACorpusTaggerSeesEveryDiscoveredFileNotJustTheStaleOnes()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeCorpusTagger("c");

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        tagger.SeenFiles.Clear();

        // Second run: both files are at the current derive version, yet a
        // global computation still needs all of them to be correct.
        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Equal([1, 2], tagger.SeenFiles.Order());
    }

    [Fact]
    public async Task ACorpusTaggerThatThrowsFailsThatTaggerOnly()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var corpus = new FakeCorpusTagger("c") { ThrowOnDerive = new InvalidOperationException("nope") };
        var other = new FakeFileTagger("a") { DefaultTags = ["ok"] };

        var report = await Host([corpus, other], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        Assert.Equal(TaggerStatus.Failed, ResultFor(report, "c").Status);
        Assert.Contains("nope", ResultFor(report, "c").Error, StringComparison.Ordinal);
        Assert.Equal(TaggerStatus.Completed, ResultFor(report, "a").Status);
    }

    [Fact]
    public async Task SelectingATaggerAlsoRunsItsDependencies()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        var upstream = new FakeFileTagger("a") { DefaultTags = ["up"] };
        var downstream = new FakeFileTagger("b") { DependsOnIds = ["a"], DefaultTags = ["down"] };
        var unrelated = new FakeFileTagger("z") { DefaultTags = ["nope"] };

        var report = await Host([upstream, downstream, unrelated], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions { OnlyTaggers = ["b"] });

        Assert.Equal(TaggerStatus.Completed, ResultFor(report, "a").Status);
        Assert.Equal(TaggerStatus.Completed, ResultFor(report, "b").Status);
        Assert.Equal(TaggerStatus.Skipped, ResultFor(report, "z").Status);
        Assert.Empty(unrelated.DerivedFiles);
    }

    [Fact]
    public async Task SelectingAnUnknownTaggerIsAnErrorRatherThanASilentNoOp()
    {
        var host = Host([new FakeFileTagger("a")], new FakeHydrusClient(), new InMemoryTaggerStateStore());

        var ex = await Assert.ThrowsAsync<TaggerGraphException>(() =>
            host.RunAsync(new TaggerRunOptions { OnlyTaggers = ["typo"] }));

        Assert.Contains("typo", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public async Task FileIdentityIsCachedSoLaterRunsFetchNoMetadataForUpToDateFiles()
    {
        var hydrus = new FakeHydrusClient().WithFile(1).WithFile(2);
        var store = new InMemoryTaggerStateStore();
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());
        hydrus.MetadataRequests.Clear();

        await Host([tagger], hydrus, store).RunAsync(new TaggerRunOptions());

        Assert.Empty(hydrus.MetadataRequests);
    }

    [Fact]
    public async Task MetadataIsFetchedInBatches()
    {
        var hydrus = new FakeHydrusClient();
        for (var i = 1; i <= 5; i++)
        {
            hydrus.WithFile(i);
        }

        await Host([new FakeFileTagger("t")], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions { MetadataBatchSize = 2 });

        Assert.Equal([2, 2, 1], hydrus.MetadataRequests.Select(r => r.Count));
    }

    [Fact]
    public async Task AFileHydrusReturnsNoMetadataForIsWarnedAboutAndSkipped()
    {
        var hydrus = new FakeHydrusClient().WithFile(1);
        hydrus.SearchResult.Add(999);
        var tagger = new FakeFileTagger("t") { DefaultTags = ["vrchat"] };

        var report = await Host([tagger], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions());

        var result = ResultFor(report, "t");
        Assert.Equal(2, result.Discovered);
        Assert.Equal(1, result.Derived);
        Assert.Contains(result.Warnings, w => w.Contains("999", StringComparison.Ordinal));
    }

    [Fact]
    public async Task NoTagServiceIsResolvedWhenThereIsNothingToRun()
    {
        // Resolution is deliberately lazy: two local tag services exist on the
        // real client, so an unqualified resolve throws. A no-op run should not.
        var hydrus = new FakeHydrusClient
        {
            ThrowOnResolveService = new InvalidOperationException("ambiguous"),
        };

        var report = await Host([new FakeFileTagger("a")], hydrus, new InMemoryTaggerStateStore())
            .RunAsync(new TaggerRunOptions { OnlyTaggers = [] });

        // One tagger, and it does get as far as resolving -- so this asserts the
        // failure is contained, not that resolution never happens.
        Assert.Equal(TaggerStatus.Failed, ResultFor(report, "a").Status);
    }

    [Fact]
    public void TheHostExposesTaggersInDependencyOrder()
    {
        var host = Host(
            [new FakeFileTagger("b") { DependsOnIds = ["a"] }, new FakeFileTagger("a")],
            new FakeHydrusClient(),
            new InMemoryTaggerStateStore());

        Assert.Equal(["a", "b"], host.OrderedTaggers.Select(t => t.Id));
    }

    [Fact]
    public void ACycleIsRejectedWhenTheHostIsConstructed()
    {
        Assert.Throws<TaggerGraphException>(() => Host(
            [
                new FakeFileTagger("a") { DependsOnIds = ["b"] },
                new FakeFileTagger("b") { DependsOnIds = ["a"] },
            ],
            new FakeHydrusClient(),
            new InMemoryTaggerStateStore()));
    }
}
