using HydrusTagger.Core.Tagging;
using HydrusTagger.Tests.Tagging.Fakes;

namespace HydrusTagger.Tests.Tagging;

public class TaggerGraphTests
{
    [Fact]
    public void DependenciesRunBeforeTheTaggersThatNeedThem()
    {
        var order = TaggerGraph.Sort(
        [
            new FakeFileTagger("correlation") { DependsOnIds = ["vrchat"] },
            new FakeFileTagger("vrchat"),
        ]);

        Assert.Equal(["vrchat", "correlation"], order.Select(t => t.Id));
    }

    [Fact]
    public void IndependentTaggersComeOutInIdOrderRegardlessOfRegistrationOrder()
    {
        // Registration order is a DI accident; a run's shape should not be.
        var forward = TaggerGraph.Sort(
            [new FakeFileTagger("c"), new FakeFileTagger("a"), new FakeFileTagger("b")]);
        var backward = TaggerGraph.Sort(
            [new FakeFileTagger("b"), new FakeFileTagger("c"), new FakeFileTagger("a")]);

        Assert.Equal(["a", "b", "c"], forward.Select(t => t.Id));
        Assert.Equal(["a", "b", "c"], backward.Select(t => t.Id));
    }

    [Fact]
    public void TransitiveDependenciesAreOrderedCorrectly()
    {
        var order = TaggerGraph.Sort(
        [
            new FakeFileTagger("c") { DependsOnIds = ["b"] },
            new FakeFileTagger("b") { DependsOnIds = ["a"] },
            new FakeFileTagger("a"),
        ]);

        Assert.Equal(["a", "b", "c"], order.Select(t => t.Id));
    }

    [Fact]
    public void ACycleIsReportedWithThePathThroughIt()
    {
        var ex = Assert.Throws<TaggerGraphException>(() => TaggerGraph.Sort(
        [
            new FakeFileTagger("a") { DependsOnIds = ["b"] },
            new FakeFileTagger("b") { DependsOnIds = ["c"] },
            new FakeFileTagger("c") { DependsOnIds = ["a"] },
        ]));

        Assert.Contains("cycle", ex.Message, StringComparison.OrdinalIgnoreCase);
        Assert.Contains("a -> b -> c -> a", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void ATaggerDependingOnItselfIsACycle()
    {
        var ex = Assert.Throws<TaggerGraphException>(() => TaggerGraph.Sort(
            [new FakeFileTagger("a") { DependsOnIds = ["a"] }]));

        Assert.Contains("a -> a", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void AnUnknownDependencyIsRejectedRatherThanIgnored()
    {
        var ex = Assert.Throws<TaggerGraphException>(() => TaggerGraph.Sort(
            [new FakeFileTagger("a") { DependsOnIds = ["nope"] }]));

        Assert.Contains("nope", ex.Message, StringComparison.Ordinal);
        Assert.Contains("not registered", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void DuplicateIdsAreRejectedBecauseIdsScopeDatabaseRows()
    {
        var ex = Assert.Throws<TaggerGraphException>(() => TaggerGraph.Sort(
            [new FakeFileTagger("a"), new FakeFileTagger("a")]));

        Assert.Contains("share the id 'a'", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void AnEmptyRegistrationIsNotAnError()
    {
        Assert.Empty(TaggerGraph.Sort([]));
    }
}
