using HydrusTagger.Core.Hydrus;

namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Common shape of every tagger. A bare <see cref="ITagger"/> does nothing on
/// its own; implementations also implement <see cref="IFileTagger"/> or
/// <see cref="ICorpusTagger"/>, and optionally <see cref="IFileExtractor"/>.
/// </summary>
public interface ITagger
{
    /// <summary>
    /// Stable identifier, e.g. <c>vrchat</c>. Scopes this tagger's rows in the
    /// database, so renaming one orphans its state.
    /// </summary>
    string Id { get; }

    /// <summary>
    /// Version of the derive stage. Bump whenever the tag output could change;
    /// files below the current version are re-derived. Cheap -- no disk I/O.
    /// </summary>
    int DeriveVersion { get; }

    /// <summary>Hydrus search predicates selecting this tagger's candidates.</summary>
    IReadOnlyList<string> SelectorQuery { get; }

    /// <summary>
    /// Ids of taggers whose output this one reads. The host runs dependencies
    /// first and exposes their tags via <see cref="TaggerContext.UpstreamTags"/>.
    /// </summary>
    IReadOnlyList<string> DependsOn => [];
}

/// <summary>
/// A tagger that must read file bytes off disk. Separately versioned because
/// that read is the expensive part -- 91k files on a network share -- and
/// should not be repeated just because parsing improved.
/// </summary>
public interface IFileExtractor : ITagger
{
    /// <summary>
    /// Version of the extract stage. Bump only when the on-disk read itself
    /// changes; every bump costs a full re-read of the corpus.
    /// </summary>
    int ExtractVersion { get; }

    /// <summary>
    /// Read <paramref name="file"/> and cache whatever the derive stage will
    /// need. Implementations own their own cache table.
    /// </summary>
    Task<ExtractResult> ExtractAsync(FileRef file, CancellationToken ct);
}

/// <summary>
/// A tagger that decides one file's tags from that file alone (plus cached
/// artifacts and upstream taggers' output). Never touches disk.
/// </summary>
public interface IFileTagger : ITagger
{
    Task<TagSet> DeriveAsync(TaggerContext context, CancellationToken ct);
}

/// <summary>
/// A tagger that needs the whole corpus at once -- correlation across files
/// cannot be decided one file at a time.
/// </summary>
/// <remarks>
/// The host always hands a corpus tagger every discovered file, not just the
/// version-stale ones: a global computation over a subset would be wrong.
/// </remarks>
public interface ICorpusTagger : ITagger
{
    Task<IReadOnlyDictionary<int, TagSet>> DeriveAllAsync(Corpus corpus, CancellationToken ct);
}

/// <summary>Outcome of one file's extract stage.</summary>
public sealed record ExtractResult(bool Success, string? Error = null)
{
    public static ExtractResult Ok { get; } = new(true);

    public static ExtractResult Failed(string error) => new(false, error);
}

/// <summary>Everything a tagger may consult about a single file.</summary>
public sealed class TaggerContext
{
    public required FileRef File { get; init; }

    /// <summary>
    /// Live Hydrus metadata: known URLs, current tags, dimensions. Fetched only
    /// for files that actually need deriving.
    /// </summary>
    public required HydrusFileMetadata Metadata { get; init; }

    /// <summary>
    /// Tags produced by declared dependencies for this file, keyed by tagger id.
    /// A dependency that produced nothing for this file is absent.
    /// </summary>
    public required IReadOnlyDictionary<string, TagSet> UpstreamTags { get; init; }
}

/// <summary>The whole discovered file set, for <see cref="ICorpusTagger"/>.</summary>
public sealed class Corpus
{
    public required IReadOnlyList<TaggerContext> Files { get; init; }
}
