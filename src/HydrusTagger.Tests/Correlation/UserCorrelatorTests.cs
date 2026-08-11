using HydrusTagger.Taggers.Correlation;

namespace HydrusTagger.Tests.Correlation;

/// <summary>Port of <c>tests/test_user_correlation.py</c>.</summary>
public class UserCorrelatorTests
{
    private static (IReadOnlySet<string>, IReadOnlySet<string>) F(string[] ids, string[] names) =>
        (new HashSet<string>(ids, StringComparer.Ordinal), new HashSet<string>(names, StringComparer.Ordinal));

    [Fact]
    public void TreatsIdenticalFileSetsAsAlreadyPaired()
    {
        var result = UserCorrelator.Correlate([
            F(["A"], ["Alice"]),
            F(["A"], ["Alice"]),
        ]);

        Assert.Contains(("A", "Alice"), result.PairedPairs);
        Assert.Empty(result.Suggestions);
    }

    [Fact]
    public void SuggestsAPairWhoseFileSetsMostlyOverlap()
    {
        // The name failed to parse on one of A's four files.
        var result = UserCorrelator.Correlate(
            [
                F(["A"], ["Alice"]),
                F(["A"], ["Alice"]),
                F(["A"], ["Alice"]),
                F(["A"], []),
            ],
            new CorrelationOptions { MinOverlap = 2, MinJaccard = 0.5 });

        Assert.Empty(result.PairedPairs);
        var s = Assert.Single(result.Suggestions);
        Assert.Equal("A", s.UserId);
        Assert.Equal("Alice", s.Name);
        Assert.Equal(3, s.Overlap);
        Assert.Equal(4, s.IdFiles);
        Assert.Equal(3, s.NameFiles);
    }

    [Fact]
    public void RejectsPairsBelowTheOverlapThreshold()
    {
        var result = UserCorrelator.Correlate(
            [
                F(["A"], ["Alice"]),
                F(["A"], []),
            ],
            new CorrelationOptions { MinOverlap = 2 });

        Assert.Empty(result.Suggestions);
        Assert.Single(result.Rejected);
    }

    [Fact]
    public void RejectsWhenTheRunnerUpIsJustAsGood()
    {
        // A co-occurs equally with Alice and Beth, so there is no clear winner.
        var result = UserCorrelator.Correlate(
            [
                F(["A"], ["Alice", "Beth"]),
                F(["A"], ["Alice", "Beth"]),
                F(["A"], ["Alice", "Beth"]),
            ],
            new CorrelationOptions { MinOverlap = 2, MinJaccard = 0.3, MaxRunnerUpRatio = 0.6 });

        Assert.Empty(result.Suggestions);
    }

    [Fact]
    public void RequiresAMutualBestMatch()
    {
        // B shares far more files with Alice than A does, so A must not take her.
        var result = UserCorrelator.Correlate(
            [
                F(["A"], ["Alice"]),
                F(["B"], ["Alice"]),
                F(["B"], ["Alice"]),
                F(["B"], ["Alice"]),
                F(["B"], []),
            ],
            new CorrelationOptions { MinOverlap = 1, MinJaccard = 0.1, MaxRunnerUpRatio = 1.0 });

        Assert.All(
            result.Suggestions.Where(s => s.Name == "Alice"),
            s => Assert.Equal("B", s.UserId));
    }

    [Fact]
    public void DetectsOrphansOnBothSides()
    {
        var result = UserCorrelator.Correlate([
            F(["A"], []),
            F([], ["Lonely"]),
        ]);

        Assert.Contains(("A", 1), result.OrphanIds);
        Assert.Contains(("Lonely", 1), result.OrphanNames);
        Assert.Empty(result.Suggestions);
    }

    [Fact]
    public void RefusesToGuessBetweenInseparableIds()
    {
        // A and B appear on exactly the same files, so co-occurrence cannot say
        // which is Alice and which is Bob.
        var result = UserCorrelator.Correlate(
            [
                F(["A", "B"], ["Alice", "Bob"]),
                F(["A", "B"], ["Alice", "Bob"]),
                F(["A", "B"], ["Alice"]),
            ],
            new CorrelationOptions { MinOverlap = 2, MinJaccard = 0.5 });

        Assert.Empty(result.PairedPairs);
        Assert.Empty(result.Suggestions);
    }

    [Fact]
    public void SeparatesUsersOnceOneAppearsAlone()
    {
        // B is seen once alone with Bob, which breaks the symmetry: A<->Alice
        // becomes an exclusive perfect overlap and B<->Bob is then inferred.
        var result = UserCorrelator.Correlate(
            [
                F(["A", "B"], ["Alice", "Bob"]),
                F(["A", "B"], ["Alice", "Bob"]),
                F(["A", "B"], ["Alice"]),
                F(["B"], ["Bob"]),
            ],
            new CorrelationOptions { MinOverlap = 2, MinJaccard = 0.5 });

        Assert.Contains(("A", "Alice"), result.PairedPairs);
        Assert.Contains(result.Suggestions, s => s is { UserId: "B", Name: "Bob" });
    }

    [Fact]
    public void ReportsCorpusCounts()
    {
        var result = UserCorrelator.Correlate([
            F(["A"], ["Alice"]),
            F(["B"], ["Beth"]),
            F([], []),
        ]);

        Assert.Equal(3, result.FileCount);
        Assert.Equal(2, result.IdCount);
        Assert.Equal(2, result.NameCount);
    }

    [Fact]
    public void RanksSuggestionsByConfidence()
    {
        // A/Alice overlap perfectly bar one file; C/Carol are far weaker.
        var result = UserCorrelator.Correlate(
            [
                F(["A"], ["Alice"]),
                F(["A"], ["Alice"]),
                F(["A"], ["Alice"]),
                F(["A"], []),
                F(["C"], ["Carol"]),
                F(["C"], ["Carol"]),
                F(["C"], []),
                F(["C"], []),
                F(["C"], []),
            ],
            new CorrelationOptions { MinOverlap = 2, MinJaccard = 0.1, MaxRunnerUpRatio = 1.0 });

        Assert.Equal(2, result.Suggestions.Count);
        Assert.True(
            result.Suggestions[0].Jaccard >= result.Suggestions[1].Jaccard,
            "suggestions must be ordered most-confident first");
        Assert.Equal("A", result.Suggestions[0].UserId);
    }

    [Fact]
    public void HandlesAnEmptyCorpus()
    {
        var result = UserCorrelator.Correlate([]);

        Assert.Equal(0, result.FileCount);
        Assert.Empty(result.Suggestions);
        Assert.Empty(result.PairedPairs);
    }
}
