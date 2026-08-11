namespace HydrusTagger.Core.Hydrus;

public interface IHydrusClient
{
    /// <summary>All local tag services (type 5), deduped by service key.</summary>
    Task<IReadOnlyList<HydrusService>> GetLocalTagServicesAsync(CancellationToken ct = default);

    /// <summary>
    /// Resolve a local tag service to its key. With a name, matches on it;
    /// without one, requires exactly one local tag service to exist.
    /// </summary>
    Task<string> ResolveLocalTagServiceKeyAsync(string? serviceName, CancellationToken ct = default);

    /// <summary>Run a Hydrus search and return matching file ids.</summary>
    Task<IReadOnlyList<int>> SearchFileIdsAsync(
        IReadOnlyList<string> tags,
        string? tagServiceKey = null,
        CancellationToken ct = default);

    /// <summary>
    /// Fetch metadata for the given file ids. The caller is responsible for
    /// batching; this issues exactly one request.
    /// </summary>
    Task<IReadOnlyList<HydrusFileMetadata>> GetFileMetadataAsync(
        IReadOnlyList<int> fileIds,
        CancellationToken ct = default);

    /// <summary>
    /// Add one tag set to many files in a single request. Hydrus applies the
    /// same tags to every hash, so callers group files by identical tag set.
    /// </summary>
    Task AddTagsAsync(
        IReadOnlyList<string> hashes,
        string serviceKey,
        IReadOnlyList<string> tags,
        CancellationToken ct = default);
}
