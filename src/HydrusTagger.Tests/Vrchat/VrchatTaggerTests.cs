using HydrusTagger.Core.Hydrus;
using HydrusTagger.Core.Tagging;
using HydrusTagger.Taggers.Vrchat;
using HydrusTagger.Tests.Png;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;

namespace HydrusTagger.Tests.Vrchat;

public class VrchatTaggerTests : IDisposable
{
    private const string Hash = "ab" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd";

    private readonly string _dataDir = Directory.CreateTempSubdirectory("hydrus-tagger-tests").FullName;
    private readonly FakeChunkStore _store = new();

    public void Dispose()
    {
        Directory.Delete(_dataDir, recursive: true);
        GC.SuppressFinalize(this);
    }

    private VrchatTagger Tagger(string? dataDirectory = null) => new(
        _store,
        Options.Create(new VrchatTaggerOptions { DataDirectory = dataDirectory ?? _dataDir }),
        NullLogger<VrchatTagger>.Instance);

    private static FileRef File(int fileId = 1) => new(fileId, Hash, "png");

    private void WritePng(FileRef file, params (string Type, byte[] Data)[] chunks)
    {
        var path = file.PathUnder(_dataDir);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        System.IO.File.WriteAllBytes(path, PngBuilder.Png(chunks));
    }

    private static TaggerContext Context(FileRef file) => new()
    {
        File = file,
        Metadata = new HydrusFileMetadata { FileId = file.FileId, Hash = file.Hash },
        UpstreamTags = new Dictionary<string, TagSet>(StringComparer.Ordinal),
    };

    [Fact]
    public void KeepsTheLegacyParserVersionsSoAlreadyProcessedFilesAreNotRedone()
    {
        // These match FILE_PARSER_VERSION and DATA_PARSER_VERSION in
        // core/constants.py. Raising either re-does work the Python already did.
        var tagger = Tagger();

        Assert.Equal("vrchat", tagger.Id);
        Assert.Equal(1, tagger.ExtractVersion);
        Assert.Equal(5, tagger.DeriveVersion);
    }

    [Fact]
    public async Task ExtractCachesChunksWithTheirDetectedContentType()
    {
        var file = File();
        WritePng(
            file,
            ("iTXt", PngBuilder.Itxt("Description", text: """{"application":"VRCX"}""")),
            ("iTXt", PngBuilder.Itxt("XML:com.adobe.xmp", text: "<x/>")),
            ("iTXt", PngBuilder.Itxt("Comment", text: "created with GIMP")));

        var result = await Tagger().ExtractAsync(file, default);

        Assert.True(result.Success);
        var chunks = _store.Chunks[file.FileId];
        Assert.Equal(
            [VrcContentType.Json, VrcContentType.Xml, VrcContentType.Text],
            chunks.Select(c => c.ContentType));
        Assert.Equal([0, 1, 2], chunks.Select(c => c.Seq));
    }

    [Fact]
    public async Task ExtractRecordsAnEmptyResultForAPngWithNoItxt()
    {
        // "This file has no metadata" is an answer worth caching -- it is what
        // stops the next run re-reading the file off the share.
        var file = File();
        WritePng(file, ("IDAT", [1, 2, 3, 4]));

        var result = await Tagger().ExtractAsync(file, default);

        Assert.True(result.Success);
        Assert.Empty(_store.Chunks[file.FileId]);
    }

    [Fact]
    public async Task ExtractFailsWithoutRecordingWhenTheFileIsMissing()
    {
        // Leaving nothing recorded is deliberate: a disconnected share should
        // be retried, not marked done.
        var result = await Tagger().ExtractAsync(File(), default);

        Assert.False(result.Success);
        Assert.NotNull(result.Error);
        Assert.Empty(_store.Chunks);
    }

    [Fact]
    public async Task ExtractFailsWhenNoDataDirectoryIsConfigured()
    {
        var result = await Tagger(dataDirectory: "").ExtractAsync(File(), default);

        Assert.False(result.Success);
        Assert.Contains("data directory", result.Error, StringComparison.Ordinal);
    }

    [Fact]
    public async Task ExtractPrefersTheDirectoryRecordedAgainstTheFile()
    {
        // Files imported before the current configuration may live elsewhere.
        var file = File();
        WritePng(file, ("iTXt", PngBuilder.Itxt("Description", text: """{"a":1}""")));
        _store.DataDirectories[file.FileId] = _dataDir;

        var result = await Tagger(dataDirectory: @"X:\does-not-exist").ExtractAsync(file, default);

        Assert.True(result.Success);
        Assert.Single(_store.Chunks[file.FileId]);
    }

    [Fact]
    public async Task DeriveBuildsTagsFromCachedChunks()
    {
        var file = File();
        _store.Chunks[file.FileId] =
        [
            new VrcStoredChunk(
                0, "Description", 0, 0, "", "",
                """{"author":{"id":"usr_a","displayName":"A"},"world":{"id":"wrld_b","name":"B","instanceId":""},"players":[]}""",
                VrcContentType.Json),
        ];

        var tags = await Tagger().DeriveAsync(Context(file), default);

        Assert.Contains("vrchat", tags.Tags);
        Assert.Contains("vrchat-author-id:usr_a", tags.Tags);
        Assert.Contains("vrchat-world-name:B", tags.Tags);
    }

    [Fact]
    public async Task DeriveReturnsNoTagsWhenNothingWasCached()
    {
        Assert.True((await Tagger().DeriveAsync(Context(File()), default)).IsEmpty);
    }

    [Fact]
    public async Task DeriveReturnsNoTagsForChunksThatCarryNoVrchatMetadata()
    {
        // An empty tag set means the host pushes nothing, which is the correct
        // outcome for the ~76% of chunks that are non-VRChat Adobe packets.
        var file = File();
        _store.Chunks[file.FileId] =
        [
            new VrcStoredChunk(0, "Description", 0, 0, "", "", "created with GIMP", VrcContentType.Text),
        ];

        Assert.True((await Tagger().DeriveAsync(Context(file), default)).IsEmpty);
    }

    private sealed class FakeChunkStore : IVrchatChunkStore
    {
        public Dictionary<int, List<VrcStoredChunk>> Chunks { get; } = [];

        public Dictionary<int, string> DataDirectories { get; } = [];

        public Task<IReadOnlyList<VrcChunk>> GetChunksAsync(int fileId, CancellationToken ct) =>
            Task.FromResult<IReadOnlyList<VrcChunk>>(
                Chunks.TryGetValue(fileId, out var stored)
                    ? [.. stored.Select(c => new VrcChunk(c.Keyword, c.Text, c.ContentType))]
                    : []);

        public Task ReplaceChunksAsync(int fileId, IReadOnlyList<VrcStoredChunk> chunks, CancellationToken ct)
        {
            Chunks[fileId] = [.. chunks];
            return Task.CompletedTask;
        }

        public Task<string?> GetDataDirectoryAsync(int fileId, CancellationToken ct) =>
            Task.FromResult(DataDirectories.GetValueOrDefault(fileId));
    }
}
