namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Non-persistent <see cref="ITaggerStateStore"/> for tests and for
/// <c>--dry-run</c> against a database that must not be written to.
/// </summary>
/// <remarks>
/// Thread-safe because the host may extract several files concurrently.
/// </remarks>
public sealed class InMemoryTaggerStateStore : ITaggerStateStore
{
    private readonly Lock _gate = new();
    private readonly Dictionary<int, FileRef> _files = [];
    private readonly Dictionary<(string Tagger, int FileId), TaggerFileState> _states = [];
    private readonly Dictionary<(string Tagger, int FileId), TagSet> _tags = [];
    private readonly Dictionary<(string Tagger, int FileId), string> _pushes = [];

    public Task<IReadOnlyDictionary<int, FileRef>> GetFileRefsAsync(
        IReadOnlyCollection<int> fileIds, CancellationToken ct)
    {
        lock (_gate)
        {
            IReadOnlyDictionary<int, FileRef> result = fileIds
                .Where(_files.ContainsKey)
                .ToDictionary(id => id, id => _files[id]);
            return Task.FromResult(result);
        }
    }

    public Task UpsertFileRefsAsync(IReadOnlyCollection<FileRef> files, CancellationToken ct)
    {
        lock (_gate)
        {
            foreach (var file in files)
            {
                _files[file.FileId] = file;
            }
        }

        return Task.CompletedTask;
    }

    public Task<IReadOnlyDictionary<int, TaggerFileState>> GetFileStatesAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct) =>
        Task.FromResult(Lookup(_states, taggerId, fileIds));

    public Task<IReadOnlyDictionary<int, string>> GetPushedHashesAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct) =>
        Task.FromResult(Lookup(_pushes, taggerId, fileIds));

    public Task<IReadOnlyDictionary<int, TagSet>> GetDerivedTagsAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct) =>
        Task.FromResult(Lookup(_tags, taggerId, fileIds));

    public Task<IReadOnlyDictionary<int, TagSet>> GetUnpushedTagsAsync(
        string taggerId, IReadOnlyCollection<int> fileIds, CancellationToken ct)
    {
        lock (_gate)
        {
            var result = new Dictionary<int, TagSet>();
            foreach (var id in fileIds)
            {
                var key = (taggerId, id);
                if (_tags.TryGetValue(key, out var tags) &&
                    _pushes.GetValueOrDefault(key) != tags.Hash)
                {
                    result[id] = tags;
                }
            }

            return Task.FromResult<IReadOnlyDictionary<int, TagSet>>(result);
        }
    }

    public Task RecordAsync(
        string taggerId, IReadOnlyCollection<TaggerFileOutcome> outcomes, CancellationToken ct)
    {
        lock (_gate)
        {
            foreach (var outcome in outcomes)
            {
                var key = (taggerId, outcome.FileId);

                if (outcome.ExtractVersion is not null || outcome.DeriveVersion is not null)
                {
                    var current = _states.GetValueOrDefault(key, TaggerFileState.Never);
                    _states[key] = new TaggerFileState(
                        outcome.ExtractVersion ?? current.ExtractVersion,
                        outcome.DeriveVersion ?? current.DeriveVersion);
                }

                if (outcome.Tags is not null)
                {
                    _tags[key] = outcome.Tags;
                }

                if (outcome.PushedHash is not null)
                {
                    _pushes[key] = outcome.PushedHash;
                }
            }
        }

        return Task.CompletedTask;
    }

    private IReadOnlyDictionary<int, TValue> Lookup<TValue>(
        Dictionary<(string, int), TValue> source, string taggerId, IReadOnlyCollection<int> fileIds)
    {
        lock (_gate)
        {
            var result = new Dictionary<int, TValue>(fileIds.Count);
            foreach (var id in fileIds)
            {
                if (source.TryGetValue((taggerId, id), out var value))
                {
                    result[id] = value;
                }
            }

            return result;
        }
    }
}
