namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Everything the host persists between runs: file identity, per-tagger
/// versions, derived tags, and the push ledger.
/// </summary>
/// <remarks>
/// Kept behind an interface so the host can be tested without a database, and
/// so the storage layer can change shape -- the <c>AddTaggerScope</c> migration
/// still has to add <c>tagger_id</c> to the push and tag tables -- without the
/// host noticing.
/// </remarks>
public interface ITaggerStateStore
{
    /// <summary>Known identity for the given ids. Missing ids are simply absent.</summary>
    Task<IReadOnlyDictionary<int, FileRef>> GetFileRefsAsync(
        IReadOnlyCollection<int> fileIds, CancellationToken ct);

    /// <summary>Record identity for files seen for the first time.</summary>
    Task UpsertFileRefsAsync(IReadOnlyCollection<FileRef> files, CancellationToken ct);

    /// <summary>
    /// Per-file extract/derive versions for one tagger. A file with no row has
    /// never been processed by it and is treated as version 0.
    /// </summary>
    Task<IReadOnlyDictionary<int, TaggerFileState>> GetFileStatesAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct);

    /// <summary>Hash of the tag set last pushed, per file, for one tagger.</summary>
    Task<IReadOnlyDictionary<int, string>> GetPushedHashesAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct);

    /// <summary>
    /// Tags a tagger derived on a previous run. Lets a dependent tagger read
    /// upstream output even when the upstream had nothing to re-derive.
    /// </summary>
    Task<IReadOnlyDictionary<int, TagSet>> GetDerivedTagsAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct);

    /// <summary>
    /// Stored tags whose hash does not match the push ledger -- never pushed,
    /// or pushed and then failed.
    /// </summary>
    /// <remarks>
    /// Without this the derive-version gate swallows retries: a file that
    /// derived cleanly but whose push failed is already at the current version,
    /// so the next run would not re-derive it and would never notice the gap.
    /// </remarks>
    Task<IReadOnlyDictionary<int, TagSet>> GetUnpushedTagsAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct);

    /// <summary>
    /// Commit a run's outcomes for one tagger: versions, derived tags, and push
    /// hashes. Implementations should apply this atomically.
    /// </summary>
    Task RecordAsync(
        string taggerId, IReadOnlyCollection<TaggerFileOutcome> outcomes, CancellationToken ct);
}

/// <summary>Where one file stands with respect to one tagger.</summary>
public readonly record struct TaggerFileState(int ExtractVersion, int DeriveVersion)
{
    public static TaggerFileState Never { get; } = new(0, 0);
}

/// <summary>What a run did to one file, for one tagger.</summary>
/// <param name="FileId">The file.</param>
/// <param name="ExtractVersion">Extract version now satisfied, or null to leave unchanged.</param>
/// <param name="DeriveVersion">Derive version now satisfied, or null to leave unchanged.</param>
/// <param name="Tags">Newly derived tags, or null if the derive stage was skipped.</param>
/// <param name="PushedHash">
/// Tag hash successfully pushed to Hydrus, or null if nothing was pushed. Kept
/// separate from <paramref name="Tags"/> so a failed push does not record a
/// hash that would suppress the retry.
/// </param>
public sealed record TaggerFileOutcome(
    int FileId,
    int? ExtractVersion = null,
    int? DeriveVersion = null,
    TagSet? Tags = null,
    string? PushedHash = null);
