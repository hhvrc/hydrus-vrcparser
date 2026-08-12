using HydrusTagger.Core.Png;
using HydrusTagger.Core.Tagging;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// Reads VRChat metadata out of PNG iTXt chunks and turns it into Hydrus tags.
/// </summary>
/// <remarks>
/// <para>
/// Split across both stages the host offers. Extraction reads bytes off a
/// network share and is versioned separately for that reason; deriving works
/// entirely from the cached chunks, so improving the parsers costs nothing but
/// CPU.
/// </para>
/// <para>
/// This replaces stages 1-6 of the legacy <c>hydrus-vrcparser.py</c> pipeline;
/// discovery, change detection and pushing now belong to the host.
/// </para>
/// </remarks>
public sealed class VrchatTagger : IFileExtractor, IFileTagger
{
    public const string TaggerId = "vrchat";

    private readonly IVrchatChunkStore _store;
    private readonly VrchatTaggerOptions _options;
    private readonly ILogger<VrchatTagger> _logger;

    public VrchatTagger(
        IVrchatChunkStore store,
        IOptions<VrchatTaggerOptions> options,
        ILogger<VrchatTagger> logger)
    {
        ArgumentNullException.ThrowIfNull(options);

        _store = store;
        _options = options.Value;
        _logger = logger;
    }

    public string Id => TaggerId;

    /// <summary>
    /// Matches the legacy <c>FILE_PARSER_VERSION</c>, so files already
    /// extracted by the Python are not re-read off the share.
    /// </summary>
    public int ExtractVersion => 1;

    /// <summary>
    /// Matches the legacy <c>DATA_PARSER_VERSION</c>. v5 recovered VRCX JSON
    /// embedded in the <c>dc:description</c> of Adobe-edited screenshots.
    /// </summary>
    public int DeriveVersion => 5;

    public IReadOnlyList<string> SelectorQuery => _options.SelectorQuery;

    public async Task<ExtractResult> ExtractAsync(FileRef file, CancellationToken ct)
    {
        ArgumentNullException.ThrowIfNull(file);

        var directory = await _store.GetDataDirectoryAsync(file.FileId, ct).ConfigureAwait(false)
            ?? _options.DataDirectory;

        if (string.IsNullOrWhiteSpace(directory))
        {
            return ExtractResult.Failed("no data directory configured");
        }

        var path = file.PathUnder(directory);
        var result = PngITxtReader.ReadFile(path);

        if (result.Error is not null)
        {
            // An I/O error is environmental -- a disconnected share, a file
            // Hydrus moved. Leaving the version unrecorded retries it next run,
            // which is what the legacy pipeline did too.
            _logger.LogDebug("Could not read {Path}: {Error}", path, result.Error);
            return ExtractResult.Failed(result.Error);
        }

        var chunks = new List<VrcStoredChunk>(result.Records.Count);
        foreach (var record in result.Records)
        {
            if (record.IsUnparseable)
            {
                continue;
            }

            chunks.Add(new VrcStoredChunk(
                record.Seq,
                record.Keyword,
                record.CompressionFlag,
                record.CompressionMethod,
                record.LanguageTag,
                record.TranslatedKeyword,
                record.Text,
                VrcContentType.Detect(record.Text, record.Keyword) ?? VrcContentType.Text));
        }

        // Written even when empty: that is a real answer -- this PNG carries no
        // iTXt -- and recording it is what stops the next run re-reading it.
        await _store.ReplaceChunksAsync(file.FileId, chunks, ct).ConfigureAwait(false);

        return ExtractResult.Ok;
    }

    public async Task<TagSet> DeriveAsync(TaggerContext context, CancellationToken ct)
    {
        ArgumentNullException.ThrowIfNull(context);

        var chunks = await _store.GetChunksAsync(context.File.FileId, ct).ConfigureAwait(false);
        if (chunks.Count == 0)
        {
            return TagSet.Empty;
        }

        var meta = VrchatMetaLoader.Load(chunks);

        // No chunk yielded VRChat metadata. Common and not an error: three
        // quarters of the cached chunks are Adobe XMP packets from images that
        // were never VRChat screenshots.
        return meta is null ? TagSet.Empty : new TagSet(VrchatTagBuilder.BuildFileTags(meta));
    }
}
