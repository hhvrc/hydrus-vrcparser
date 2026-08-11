using HydrusTagger.Core.Data;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Tests.Data;

public class TaggerDbContextTests : IDisposable
{
    private readonly SqliteConnection _connection;
    private readonly DbContextOptions<TaggerDbContext> _options;

    public TaggerDbContextTests()
    {
        // A shared in-memory database lives as long as the connection does.
        _connection = new SqliteConnection("DataSource=:memory:;Foreign Keys=True");
        _connection.Open();

        _options = new DbContextOptionsBuilder<TaggerDbContext>()
            .UseSqlite(_connection)
            .Options;

        using var ctx = new TaggerDbContext(_options);
        ctx.Database.EnsureCreated();
    }

    private TaggerDbContext NewContext() => new(_options);

    public void Dispose()
    {
        _connection.Dispose();
        GC.SuppressFinalize(this);
    }

    private static FileRecord SampleFile(int fileId = 1, string hash = "aa") => new()
    {
        FileId = fileId,
        Hash = hash,
        FileExt = "png",
        DataDirId = 1,
        CreatedAt = new DateTimeOffset(2026, 2, 15, 17, 32, 13, TimeSpan.Zero),
        Size = 1234,
    };

    private void SeedDataDir()
    {
        using var ctx = NewContext();
        ctx.DataDirs.Add(new DataDir { Id = 1, Path = @"\\pve\hydrus\files" });
        ctx.SaveChanges();
    }

    [Fact]
    public void PersistsAndReadsBackAFileRecord()
    {
        SeedDataDir();

        using (var ctx = NewContext())
        {
            ctx.Files.Add(SampleFile());
            ctx.SaveChanges();
        }

        using (var ctx = NewContext())
        {
            var file = ctx.Files.AsNoTracking().Single();
            Assert.Equal(1, file.FileId);
            Assert.Equal("aa", file.Hash);
            Assert.Equal(new DateTimeOffset(2026, 2, 15, 17, 32, 13, TimeSpan.Zero), file.CreatedAt);
            Assert.Null(file.ParsedAt);
            Assert.Equal(0, file.FileParserVersion);
        }
    }

    [Fact]
    public void StoresTimestampsInThePythonTextFormat()
    {
        SeedDataDir();

        using (var ctx = NewContext())
        {
            ctx.Files.Add(SampleFile());
            ctx.SaveChanges();
        }

        // Read the raw column to prove the converter reached the database,
        // not just the model.
        using var cmd = _connection.CreateCommand();
        cmd.CommandText = "SELECT created_at FROM files";
        Assert.Equal("2026-02-15T17:32:13.000000+00:00", (string)cmd.ExecuteScalar()!);
    }

    [Fact]
    public void ChangeTrackerRejectsADuplicateHashBeforeItReachesTheDatabase()
    {
        // Modelling hash as an alternate key means EF catches the collision in
        // the identity map. The legacy code called sys.exit() here
        // (db_logic.py:348); this is the same invariant, enforced earlier.
        SeedDataDir();
        using var ctx = NewContext();
        ctx.Files.Add(SampleFile(fileId: 1, hash: "dup"));
        ctx.SaveChanges();

        Assert.Throws<InvalidOperationException>(() => ctx.Files.Add(SampleFile(fileId: 2, hash: "dup")));
    }

    [Fact]
    public void DatabaseRejectsADuplicateHashOnADifferentFileId()
    {
        // And the UNIQUE constraint still backs it up when the write comes from
        // a context that never saw the first row.
        SeedDataDir();
        using (var ctx = NewContext())
        {
            ctx.Files.Add(SampleFile(fileId: 1, hash: "dup"));
            ctx.SaveChanges();
        }

        using (var ctx = NewContext())
        {
            ctx.Files.Add(SampleFile(fileId: 2, hash: "dup"));
            Assert.Throws<DbUpdateException>(() => ctx.SaveChanges());
        }
    }

    [Fact]
    public void EnforcesForeignKeysSoOrphanChunksCannotBeWritten()
    {
        using var ctx = NewContext();
        ctx.ItxtChunks.Add(new ItxtChunk { FileId = 999, Seq = 0, ContentType = "json" });

        Assert.Throws<DbUpdateException>(() => ctx.SaveChanges());
    }

    [Fact]
    public void DefaultsContentTypeToText()
    {
        SeedDataDir();
        using var ctx = NewContext();
        ctx.Files.Add(SampleFile());
        ctx.SaveChanges();

        using var cmd = _connection.CreateCommand();
        cmd.CommandText = "INSERT INTO itxt_chunks(file_id, seq) VALUES (1, 0); SELECT content_type FROM itxt_chunks";
        Assert.Equal("text", (string)cmd.ExecuteScalar()!);
    }

    [Fact]
    public void SupportsCompositeKeyedTagRows()
    {
        SeedDataDir();
        using var ctx = NewContext();
        ctx.Files.Add(SampleFile());
        ctx.TagMappings.Add(new TagMapping { Parent = "vrchat-user-id:usr_1", Child = "vrchat-user-name:Alice" });
        ctx.HashTags.Add(new HashTag { FileId = 1, Tag = "vrchat" });
        ctx.HashTags.Add(new HashTag { FileId = 1, Tag = "vrchat-world-name:Home" });
        ctx.SaveChanges();

        Assert.Equal(2, ctx.HashTags.Count());
        Assert.Single(ctx.TagMappings);
    }

    [Fact]
    public void CreatesTheLegacyIndexNames()
    {
        using var cmd = _connection.CreateCommand();
        cmd.CommandText =
            "SELECT name FROM sqlite_master WHERE type='index' AND name LIKE 'idx_%' ORDER BY name";
        using var reader = cmd.ExecuteReader();

        var names = new List<string>();
        while (reader.Read())
        {
            names.Add(reader.GetString(0));
        }

        Assert.Equal(["idx_files_data_dir_id", "idx_files_hash", "idx_hash_tags_file_id"], names);
    }
}
