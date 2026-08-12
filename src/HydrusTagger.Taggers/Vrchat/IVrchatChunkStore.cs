namespace HydrusTagger.Taggers.Vrchat;

/// <summary>One extracted iTXt chunk, ready to cache.</summary>
public sealed record VrcStoredChunk(
    int Seq,
    string? Keyword,
    int? CompressionFlag,
    int? CompressionMethod,
    string? LanguageTag,
    string? TranslatedKeyword,
    string? Text,
    string ContentType);

/// <summary>
/// The VRChat tagger's own cache of raw iTXt chunks.
/// </summary>
/// <remarks>
/// This is the expensive asset in the whole system: 13,402 chunks whose only
/// other source is re-reading 91,238 PNGs off a network share. It is owned by
/// this tagger rather than the host because no other tagger needs byte-level
/// caching yet.
/// </remarks>
public interface IVrchatChunkStore
{
    /// <summary>Cached chunks for a file, in <c>seq</c> order.</summary>
    Task<IReadOnlyList<VrcChunk>> GetChunksAsync(int fileId, CancellationToken ct);

    /// <summary>Replace a file's cached chunks wholesale.</summary>
    Task ReplaceChunksAsync(int fileId, IReadOnlyList<VrcStoredChunk> chunks, CancellationToken ct);

    /// <summary>
    /// The data directory this file was recorded under, if one is known.
    /// Files predating the current configuration may live elsewhere.
    /// </summary>
    Task<string?> GetDataDirectoryAsync(int fileId, CancellationToken ct);
}
