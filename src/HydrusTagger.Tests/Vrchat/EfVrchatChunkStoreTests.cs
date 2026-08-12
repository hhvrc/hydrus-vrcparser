using HydrusTagger.Core.Data;
using HydrusTagger.Taggers.Vrchat;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>
/// Exercises the store against a real SQLite file built from the migrations,
/// so the mapping of <c>itxt_chunks</c> is checked rather than assumed.
/// </summary>
public class EfVrchatChunkStoreTests : IDisposable
{
    private readonly string _databasePath =
        Path.Combine(Directory.CreateTempSubdirectory("hydrus-tagger-store").FullName, "test.db");

    private readonly TestContextFactory _factory;

    public EfVrchatChunkStoreTests()
    {
        _factory = new TestContextFactory(_databasePath);

        using var db = _factory.CreateDbContext();
        db.Database.Migrate();

        db.DataDirs.Add(new DataDir { Id = 1, Path = @"\\pve\hydrus\files" });
        db.Files.Add(new FileRecord
        {
            FileId = 42,
            Hash = new string('a', 64),
            FileExt = "png",
            DataDirId = 1,
            CreatedAt = DateTimeOffset.UtcNow,
            Size = 123,
        });
        db.SaveChanges();
    }

    public void Dispose()
    {
        // Microsoft.Data.Sqlite pools connections, so the file stays open after
        // the last context is disposed and Windows refuses to delete it.
        SqliteConnection.ClearAllPools();

        try
        {
            Directory.Delete(Path.GetDirectoryName(_databasePath)!, recursive: true);
        }
        catch (IOException)
        {
            // A leaked handle should not turn into a failing test; the temp
            // directory is the OS's problem after that.
        }

        GC.SuppressFinalize(this);
    }

    private static VrcStoredChunk Chunk(int seq, string keyword, string text, string contentType) =>
        new(seq, keyword, 0, 0, "", "", text, contentType);

    [Fact]
    public async Task RoundTripsChunksInSeqOrder()
    {
        using var store = new EfVrchatChunkStore(_factory);

        await store.ReplaceChunksAsync(42,
        [
            Chunk(1, "XML:com.adobe.xmp", "<x/>", VrcContentType.Xml),
            Chunk(0, "Description", """{"a":1}""", VrcContentType.Json),
        ], default);

        var chunks = await store.GetChunksAsync(42, default);

        Assert.Equal(["Description", "XML:com.adobe.xmp"], chunks.Select(c => c.Keyword));
        Assert.Equal([VrcContentType.Json, VrcContentType.Xml], chunks.Select(c => c.ContentType));
    }

    [Fact]
    public async Task ReplacingDropsThePreviousChunksRatherThanAccumulating()
    {
        using var store = new EfVrchatChunkStore(_factory);

        await store.ReplaceChunksAsync(42, [Chunk(0, "Description", "old", VrcContentType.Text)], default);
        await store.ReplaceChunksAsync(42, [Chunk(0, "Description", "new", VrcContentType.Text)], default);

        var chunk = Assert.Single(await store.GetChunksAsync(42, default));
        Assert.Equal("new", chunk.Text);
    }

    [Fact]
    public async Task ReplacingWithNothingClearsTheFile()
    {
        using var store = new EfVrchatChunkStore(_factory);

        await store.ReplaceChunksAsync(42, [Chunk(0, "Description", "old", VrcContentType.Text)], default);
        await store.ReplaceChunksAsync(42, [], default);

        Assert.Empty(await store.GetChunksAsync(42, default));
    }

    [Fact]
    public async Task ReturnsNothingForAFileThatWasNeverExtracted()
    {
        using var store = new EfVrchatChunkStore(_factory);

        Assert.Empty(await store.GetChunksAsync(999, default));
    }

    [Fact]
    public async Task ReadsTheDataDirectoryRecordedAgainstAFile()
    {
        using var store = new EfVrchatChunkStore(_factory);

        Assert.Equal(@"\\pve\hydrus\files", await store.GetDataDirectoryAsync(42, default));
        Assert.Null(await store.GetDataDirectoryAsync(999, default));
    }

    [Fact]
    public async Task ConcurrentWritesAreSerializedRatherThanFightingOverTheSqliteLock()
    {
        // The host extracts several files at once; SQLite takes one writer.
        using var store = new EfVrchatChunkStore(_factory);

        using var db = _factory.CreateDbContext();
        for (var id = 100; id < 116; id++)
        {
            db.Files.Add(new FileRecord
            {
                FileId = id,
                Hash = id.ToString("x64", System.Globalization.CultureInfo.InvariantCulture),
                FileExt = "png",
                DataDirId = 1,
                CreatedAt = DateTimeOffset.UtcNow,
                Size = 1,
            });
        }

        await db.SaveChangesAsync();

        await Task.WhenAll(Enumerable.Range(100, 16).Select(id =>
            store.ReplaceChunksAsync(id, [Chunk(0, "Description", $"file {id}", VrcContentType.Text)], default)));

        foreach (var id in Enumerable.Range(100, 16))
        {
            var chunk = Assert.Single(await store.GetChunksAsync(id, default));
            Assert.Equal($"file {id}", chunk.Text);
        }
    }

    private sealed class TestContextFactory(string databasePath) : IDbContextFactory<TaggerDbContext>
    {
        public TaggerDbContext CreateDbContext() => new(
            new DbContextOptionsBuilder<TaggerDbContext>()
                .UseSqlite(DataServiceCollectionExtensions.BuildConnectionString(databasePath))
                .AddInterceptors(new SqlitePragmaInterceptor())
                .Options);
    }
}
