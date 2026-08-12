using HydrusTagger.Core.Hydrus;

namespace HydrusTagger.Tests.Tagging.Fakes;

/// <summary>
/// Scriptable <see cref="IHydrusClient"/> that records what the host asked it
/// to do, so tests can assert on request shape -- batching in particular.
/// </summary>
public sealed class FakeHydrusClient : IHydrusClient
{
    public List<int> SearchResult { get; set; } = [];

    public Dictionary<int, HydrusFileMetadata> Metadata { get; } = [];

    public string ServiceKey { get; set; } = "deadbeef";

    public Exception? ThrowOnResolveService { get; set; }

    public Exception? ThrowOnAddTags { get; set; }

    public List<IReadOnlyList<int>> MetadataRequests { get; } = [];

    public List<AddTagsCall> AddTagsCalls { get; } = [];

    /// <summary>Register a file that Hydrus knows about.</summary>
    public FakeHydrusClient WithFile(int fileId, string? hash = null, string ext = "png",
        IEnumerable<string>? knownUrls = null)
    {
        Metadata[fileId] = new HydrusFileMetadata
        {
            FileId = fileId,
            Hash = hash ?? $"{fileId:x2}" + new string('0', 62),
            Ext = ext,
            KnownUrls = knownUrls?.ToList(),
        };
        SearchResult.Add(fileId);
        return this;
    }

    public Task<IReadOnlyList<HydrusService>> GetLocalTagServicesAsync(CancellationToken ct = default) =>
        Task.FromResult<IReadOnlyList<HydrusService>>([new HydrusService(ServiceKey, "my tags", 5)]);

    public Task<string> ResolveLocalTagServiceKeyAsync(string? serviceName, CancellationToken ct = default)
    {
        if (ThrowOnResolveService is not null)
        {
            throw ThrowOnResolveService;
        }

        return Task.FromResult(ServiceKey);
    }

    public Task<IReadOnlyList<int>> SearchFileIdsAsync(
        IReadOnlyList<string> tags, string? tagServiceKey = null, CancellationToken ct = default) =>
        Task.FromResult<IReadOnlyList<int>>(SearchResult);

    public Task<IReadOnlyList<HydrusFileMetadata>> GetFileMetadataAsync(
        IReadOnlyList<int> fileIds, CancellationToken ct = default)
    {
        MetadataRequests.Add([.. fileIds]);
        return Task.FromResult<IReadOnlyList<HydrusFileMetadata>>(
            [.. fileIds.Where(Metadata.ContainsKey).Select(id => Metadata[id])]);
    }

    public Task AddTagsAsync(
        IReadOnlyList<string> hashes, string serviceKey, IReadOnlyList<string> tags, CancellationToken ct = default)
    {
        if (ThrowOnAddTags is not null)
        {
            throw ThrowOnAddTags;
        }

        AddTagsCalls.Add(new AddTagsCall([.. hashes], serviceKey, [.. tags]));
        return Task.CompletedTask;
    }

    public sealed record AddTagsCall(
        IReadOnlyList<string> Hashes, string ServiceKey, IReadOnlyList<string> Tags);
}
