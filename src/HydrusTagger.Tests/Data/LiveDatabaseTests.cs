using HydrusTagger.Core.Data;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Tests.Data;

/// <summary>
/// Reads a real cache database through the EF model. Skipped unless
/// <c>HYDRUSTAGGER_TEST_DB</c> points at a copy of one, so CI stays hermetic.
/// </summary>
/// <remarks>
/// Point this at a COPY, never the working database:
/// <code>
/// HYDRUSTAGGER_TEST_DB=/path/to/vrchat_copy.db dotnet test
/// </code>
/// This is the check that catches a model that compiles and passes synthetic
/// tests but cannot actually read production rows -- most likely via the
/// timestamp converter, which must cope with every value ever written.
/// </remarks>
public class LiveDatabaseTests
{
    private static string? DbPath => Environment.GetEnvironmentVariable("HYDRUSTAGGER_TEST_DB");

    private static TaggerDbContext? TryOpen()
    {
        var path = DbPath;
        if (string.IsNullOrWhiteSpace(path) || !File.Exists(path))
        {
            return null;
        }

        var options = new DbContextOptionsBuilder<TaggerDbContext>()
            .UseSqlite(DataServiceCollectionExtensions.BuildConnectionString(path))
            .Options;

        return new TaggerDbContext(options);
    }

    [SkippableFact]
    public void ReadsEveryTableWithoutConversionErrors()
    {
        using var ctx = TryOpen();
        Skip.If(ctx is null, "HYDRUSTAGGER_TEST_DB not set to an existing database.");

        // Pull every timestamp column client-side. Count()/All() would be
        // translated to SQL and never run the converter at all, so these
        // deliberately materialize: one unparseable stored value throws here.
        var fileTimes = ctx.Files.AsNoTracking()
            .Select(f => new { f.FileId, f.CreatedAt, f.ParsedAt })
            .AsEnumerable()
            .ToList();

        Assert.NotEmpty(fileTimes);
        Assert.All(fileTimes, f => Assert.True(
            f.CreatedAt.Year is >= 2020 and <= 2100,
            $"file {f.FileId} has implausible created_at {f.CreatedAt:O}"));
        Assert.All(fileTimes, f => Assert.True(
            f.ParsedAt is null || f.ParsedAt.Value.Year >= 2020,
            $"file {f.FileId} has implausible parsed_at {f.ParsedAt:O}"));

        var pushTimes = ctx.Pushes.AsNoTracking()
            .Select(p => new { p.FileId, p.FirstPushed, p.LastPushed })
            .AsEnumerable()
            .ToList();
        Assert.NotEmpty(pushTimes);
        Assert.All(pushTimes, p => Assert.True(p.LastPushed >= p.FirstPushed));

        var metaTimes = ctx.HydrusMeta.AsNoTracking().Select(m => m.UpdatedAt).AsEnumerable().ToList();
        Assert.NotEmpty(metaTimes);

        Assert.True(ctx.ItxtChunks.AsNoTracking().Any());
        Assert.NotNull(ctx.DataDirs.AsNoTracking().FirstOrDefault());
    }

    [SkippableFact]
    public void SeesTheContentTypeDistributionTheDiagnosticsReport()
    {
        using var ctx = TryOpen();
        Skip.If(ctx is null, "HYDRUSTAGGER_TEST_DB not set to an existing database.");

        var byType = ctx.ItxtChunks.AsNoTracking()
            .GroupBy(c => c.ContentType)
            .Select(g => new { Type = g.Key, Count = g.Count() })
            .ToDictionary(x => x.Type, x => x.Count, StringComparer.Ordinal);

        // Whatever the exact counts, the four known discriminators are the only
        // values that should ever appear.
        Assert.All(byType.Keys, t => Assert.Contains(t, (string[])["json", "xml", "line", "text"]));
        Assert.True(byType.Values.Sum() > 0);
    }

    [SkippableFact]
    public void HasTheBaselineMigrationStamped()
    {
        using var ctx = TryOpen();
        Skip.If(ctx is null, "HYDRUSTAGGER_TEST_DB not set to an existing database.");

        var applied = ctx.Database.GetAppliedMigrations().ToList();
        Assert.Contains(applied, m => m.EndsWith("_Baseline", StringComparison.Ordinal));
        Assert.Empty(ctx.Database.GetPendingMigrations());
    }
}
