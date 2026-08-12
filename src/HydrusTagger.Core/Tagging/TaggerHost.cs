using System.Collections.Concurrent;
using HydrusTagger.Core.Hydrus;
using Microsoft.Extensions.Logging;

namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Runs taggers in dependency order, owning everything that is not
/// tagger-specific: discovery, metadata caching, version gating, change
/// detection, batching and reporting.
/// </summary>
/// <remarks>
/// The point of this class is that a tagger only answers two questions -- which
/// files are mine, and what tags does this file get -- and inherits the rest.
/// In the Python implementation this loop lived inside the VRChat pipeline, so
/// the twitter tagger had to reimplement it and ended up with no change
/// detection at all.
/// </remarks>
public sealed class TaggerHost
{
    private const int MaxWarningsPerTagger = 20;

    private readonly IReadOnlyList<ITagger> _taggers;
    private readonly IHydrusClient _hydrus;
    private readonly ITaggerStateStore _store;
    private readonly ILogger<TaggerHost> _logger;

    public TaggerHost(
        IEnumerable<ITagger> taggers,
        IHydrusClient hydrus,
        ITaggerStateStore store,
        ILogger<TaggerHost> logger)
    {
        ArgumentNullException.ThrowIfNull(taggers);

        _taggers = TaggerGraph.Sort(taggers);
        _hydrus = hydrus;
        _store = store;
        _logger = logger;
    }

    /// <summary>Registered taggers, in the order they will run.</summary>
    public IReadOnlyList<ITagger> OrderedTaggers => _taggers;

    public async Task<TaggerRunReport> RunAsync(TaggerRunOptions options, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(options);

        var selected = SelectTaggers(options.OnlyTaggers);
        var results = new List<TaggerResult>(_taggers.Count);
        var completed = new HashSet<string>(StringComparer.Ordinal);
        var derivedThisRun = new Dictionary<string, IReadOnlyDictionary<int, TagSet>>(StringComparer.Ordinal);
        string? serviceKey = null;

        foreach (var tagger in _taggers)
        {
            if (!selected.Contains(tagger.Id))
            {
                results.Add(new TaggerResult { TaggerId = tagger.Id, Status = TaggerStatus.Skipped });
                continue;
            }

            var missing = tagger.DependsOn.Where(d => !completed.Contains(d)).ToList();
            if (missing.Count > 0)
            {
                _logger.LogWarning(
                    "Skipping tagger {TaggerId}: dependencies did not complete: {Missing}",
                    tagger.Id, string.Join(", ", missing));
                results.Add(new TaggerResult
                {
                    TaggerId = tagger.Id,
                    Status = TaggerStatus.SkippedMissingDependency,
                    Error = $"dependencies did not complete: {string.Join(", ", missing)}",
                });
                continue;
            }

            try
            {
                // Resolved once, lazily: a run with nothing to push should not
                // fail merely because the service name is ambiguous.
                serviceKey ??= await _hydrus
                    .ResolveLocalTagServiceKeyAsync(options.TagServiceName, ct)
                    .ConfigureAwait(false);

                var result = await RunTaggerAsync(tagger, options, serviceKey, derivedThisRun, ct)
                    .ConfigureAwait(false);

                results.Add(result);
                completed.Add(tagger.Id);
            }
            catch (Exception ex) when (ex is not OperationCanceledException)
            {
                // One broken tagger must not abort the others. Dependents are
                // skipped above, because their input would be stale.
                _logger.LogError(ex, "Tagger {TaggerId} failed", tagger.Id);
                results.Add(new TaggerResult
                {
                    TaggerId = tagger.Id,
                    Status = TaggerStatus.Failed,
                    Error = $"{ex.GetType().Name}: {ex.Message}",
                });
            }
        }

        return new TaggerRunReport(results);
    }

    /// <summary>
    /// Expand an explicit tagger selection to include its dependencies -- asking
    /// for the correlation tagger alone would otherwise silently produce
    /// nothing.
    /// </summary>
    private HashSet<string> SelectTaggers(IReadOnlyList<string> only)
    {
        var all = _taggers.Select(t => t.Id).ToHashSet(StringComparer.Ordinal);
        if (only.Count == 0)
        {
            return all;
        }

        var unknown = only.Where(id => !all.Contains(id)).ToList();
        if (unknown.Count > 0)
        {
            throw new TaggerGraphException(
                $"Unknown tagger(s): {string.Join(", ", unknown)}. Registered: {string.Join(", ", all.Order(StringComparer.Ordinal))}");
        }

        var byId = _taggers.ToDictionary(t => t.Id, StringComparer.Ordinal);
        var selected = new HashSet<string>(StringComparer.Ordinal);
        var queue = new Queue<string>(only);

        while (queue.Count > 0)
        {
            var id = queue.Dequeue();
            if (!selected.Add(id))
            {
                continue;
            }

            foreach (var dependency in byId[id].DependsOn)
            {
                queue.Enqueue(dependency);
            }
        }

        return selected;
    }

    private async Task<TaggerResult> RunTaggerAsync(
        ITagger tagger,
        TaggerRunOptions options,
        string serviceKey,
        Dictionary<string, IReadOnlyDictionary<int, TagSet>> derivedThisRun,
        CancellationToken ct)
    {
        var warnings = new List<string>();

        // 1. Discover.
        if (tagger.SelectorQuery.Count == 0)
        {
            throw new InvalidOperationException(
                $"Tagger '{tagger.Id}' has an empty SelectorQuery; it would match the entire Hydrus database.");
        }

        var fileIds = await _hydrus.SearchFileIdsAsync(tagger.SelectorQuery, ct: ct).ConfigureAwait(false);
        _logger.LogInformation("Tagger {TaggerId}: discovered {Count} candidate files", tagger.Id, fileIds.Count);

        if (fileIds.Count == 0)
        {
            return new TaggerResult { TaggerId = tagger.Id, Status = TaggerStatus.Completed };
        }

        // 2. Resolve identity, fetching only what is not already cached.
        var metadata = new Dictionary<int, HydrusFileMetadata>();
        var files = await ResolveFileRefsAsync(fileIds, options, metadata, warnings, ct).ConfigureAwait(false);

        var states = await _store.GetFileStatesAsync(tagger.Id, files.Keys, ct).ConfigureAwait(false);

        // 3. Extract, for taggers that read bytes off disk.
        var extracted = new HashSet<int>();
        var extractFailed = 0;
        var outcomes = new ConcurrentDictionary<int, TaggerFileOutcome>();

        if (tagger is IFileExtractor extractor)
        {
            var stale = files.Values
                .Where(f => states.GetValueOrDefault(f.FileId, TaggerFileState.Never).ExtractVersion
                            < extractor.ExtractVersion)
                .ToList();

            _logger.LogInformation(
                "Tagger {TaggerId}: extracting {Count} files (version {Version})",
                tagger.Id, stale.Count, extractor.ExtractVersion);

            var failures = new ConcurrentBag<string>();
            var succeeded = new ConcurrentBag<int>();

            await Parallel.ForEachAsync(
                stale,
                new ParallelOptions { MaxDegreeOfParallelism = options.ExtractConcurrency, CancellationToken = ct },
                async (file, token) =>
                {
                    try
                    {
                        var result = await extractor.ExtractAsync(file, token).ConfigureAwait(false);
                        if (result.Success)
                        {
                            succeeded.Add(file.FileId);
                        }
                        else
                        {
                            failures.Add($"file {file.FileId}: {result.Error}");
                        }
                    }
                    catch (Exception ex) when (ex is not OperationCanceledException)
                    {
                        failures.Add($"file {file.FileId}: {ex.GetType().Name}: {ex.Message}");
                    }
                }).ConfigureAwait(false);

            foreach (var id in succeeded)
            {
                extracted.Add(id);
                outcomes[id] = new TaggerFileOutcome(id, ExtractVersion: extractor.ExtractVersion);
            }

            extractFailed = failures.Count;
            AddWarnings(warnings, failures);
        }

        // 4. Derive.
        var toDerive = SelectForDerive(tagger, files, states, extracted);
        _logger.LogInformation("Tagger {TaggerId}: deriving tags for {Count} files", tagger.Id, toDerive.Count);

        await EnsureMetadataAsync(toDerive.Select(f => f.FileId), options, metadata, ct).ConfigureAwait(false);

        var upstream = await LoadUpstreamTagsAsync(tagger, toDerive, derivedThisRun, ct).ConfigureAwait(false);

        var contexts = new List<TaggerContext>(toDerive.Count);
        foreach (var file in toDerive)
        {
            if (!metadata.TryGetValue(file.FileId, out var meta))
            {
                warnings.Add($"file {file.FileId}: no Hydrus metadata; skipped");
                continue;
            }

            contexts.Add(new TaggerContext
            {
                File = file,
                Metadata = meta,
                UpstreamTags = upstream.GetValueOrDefault(file.FileId)
                    ?? new Dictionary<string, TagSet>(StringComparer.Ordinal),
            });
        }

        var (derived, deriveFailed) = await DeriveAsync(tagger, contexts, warnings, ct).ConfigureAwait(false);
        derivedThisRun[tagger.Id] = derived;

        foreach (var (fileId, tags) in derived)
        {
            var existing = outcomes.GetValueOrDefault(fileId) ?? new TaggerFileOutcome(fileId);
            outcomes[fileId] = existing with { DeriveVersion = tagger.DeriveVersion, Tags = tags };
        }

        // 5. Diff and push. Files that were not re-derived are still candidates
        // if their stored tags never made it to Hydrus -- otherwise a failed
        // push would be permanent, the derive gate having already been passed.
        var pushCandidates = new Dictionary<int, TagSet>(derived);
        var notDerived = files.Keys.Where(id => !derived.ContainsKey(id)).ToList();
        if (notDerived.Count > 0)
        {
            var unpushed = await _store.GetUnpushedTagsAsync(tagger.Id, notDerived, ct).ConfigureAwait(false);
            foreach (var (fileId, tags) in unpushed)
            {
                pushCandidates[fileId] = tags;
            }
        }

        var (needingUpdate, pushed, pushFailed) = await PushAsync(
            tagger, options, serviceKey, files, pushCandidates, outcomes, warnings, ct).ConfigureAwait(false);

        // 6. Record.
        if (options.DryRun)
        {
            _logger.LogInformation("Tagger {TaggerId}: dry run, not recording state", tagger.Id);
        }
        else if (!outcomes.IsEmpty)
        {
            await _store.RecordAsync(tagger.Id, [.. outcomes.Values], ct).ConfigureAwait(false);
        }

        return new TaggerResult
        {
            TaggerId = tagger.Id,
            Status = TaggerStatus.Completed,
            Discovered = fileIds.Count,
            Extracted = extracted.Count,
            ExtractFailed = extractFailed,
            Derived = derived.Count,
            DeriveFailed = deriveFailed,
            NeedingUpdate = needingUpdate,
            Pushed = pushed,
            PushFailed = pushFailed,
            Warnings = warnings,
        };
    }

    private async Task<Dictionary<int, FileRef>> ResolveFileRefsAsync(
        IReadOnlyList<int> fileIds,
        TaggerRunOptions options,
        Dictionary<int, HydrusFileMetadata> metadata,
        List<string> warnings,
        CancellationToken ct)
    {
        var cached = await _store.GetFileRefsAsync(fileIds, ct).ConfigureAwait(false);
        var files = new Dictionary<int, FileRef>(cached);

        var unknown = fileIds.Where(id => !files.ContainsKey(id)).ToList();
        if (unknown.Count == 0)
        {
            return files;
        }

        _logger.LogInformation("Fetching Hydrus metadata for {Count} previously unseen files", unknown.Count);
        await EnsureMetadataAsync(unknown, options, metadata, ct).ConfigureAwait(false);

        var discovered = new List<FileRef>(unknown.Count);
        foreach (var id in unknown)
        {
            if (!metadata.TryGetValue(id, out var meta) || string.IsNullOrEmpty(meta.Hash))
            {
                warnings.Add($"file {id}: Hydrus returned no hash; skipped");
                continue;
            }

            var file = new FileRef(id, meta.Hash, meta.NormalizedExt);
            discovered.Add(file);
            files[id] = file;
        }

        if (discovered.Count > 0)
        {
            await _store.UpsertFileRefsAsync(discovered, ct).ConfigureAwait(false);
        }

        return files;
    }

    /// <summary>Fetch metadata for any of these ids not already in hand.</summary>
    private async Task EnsureMetadataAsync(
        IEnumerable<int> fileIds,
        TaggerRunOptions options,
        Dictionary<int, HydrusFileMetadata> metadata,
        CancellationToken ct)
    {
        var wanted = fileIds.Where(id => !metadata.ContainsKey(id)).Distinct().ToList();

        foreach (var batch in wanted.Chunk(options.MetadataBatchSize))
        {
            var rows = await _hydrus.GetFileMetadataAsync(batch, ct).ConfigureAwait(false);
            foreach (var row in rows)
            {
                if (row.FileId is int id)
                {
                    metadata[id] = row;
                }
            }
        }
    }

    private static List<FileRef> SelectForDerive(
        ITagger tagger,
        Dictionary<int, FileRef> files,
        IReadOnlyDictionary<int, TaggerFileState> states,
        HashSet<int> extractedThisRun)
    {
        // A corpus tagger computes over the whole set by construction; handing
        // it only the stale files would change its answer.
        if (tagger is ICorpusTagger)
        {
            return [.. files.Values];
        }

        if (tagger is not IFileTagger)
        {
            return [];
        }

        return
        [
            .. files.Values.Where(f =>
                extractedThisRun.Contains(f.FileId) ||
                states.GetValueOrDefault(f.FileId, TaggerFileState.Never).DeriveVersion < tagger.DeriveVersion)
        ];
    }

    private async Task<Dictionary<int, Dictionary<string, TagSet>>> LoadUpstreamTagsAsync(
        ITagger tagger,
        IReadOnlyList<FileRef> files,
        Dictionary<string, IReadOnlyDictionary<int, TagSet>> derivedThisRun,
        CancellationToken ct)
    {
        var result = new Dictionary<int, Dictionary<string, TagSet>>();
        if (tagger.DependsOn.Count == 0 || files.Count == 0)
        {
            return result;
        }

        var ids = files.Select(f => f.FileId).ToList();

        foreach (var dependency in tagger.DependsOn)
        {
            // Stored tags first, then this run's -- an upstream tagger that had
            // nothing to re-derive still has output, it is just already on disk.
            var stored = await _store.GetDerivedTagsAsync(dependency, ids, ct).ConfigureAwait(false);
            var fresh = derivedThisRun.GetValueOrDefault(dependency);

            foreach (var id in ids)
            {
                var tags = fresh?.GetValueOrDefault(id) ?? stored.GetValueOrDefault(id);
                if (tags is null)
                {
                    continue;
                }

                if (!result.TryGetValue(id, out var perFile))
                {
                    perFile = new Dictionary<string, TagSet>(StringComparer.Ordinal);
                    result[id] = perFile;
                }

                perFile[dependency] = tags;
            }
        }

        return result;
    }

    private static async Task<(Dictionary<int, TagSet> Derived, int Failed)> DeriveAsync(
        ITagger tagger,
        List<TaggerContext> contexts,
        List<string> warnings,
        CancellationToken ct)
    {
        var derived = new Dictionary<int, TagSet>();

        if (tagger is ICorpusTagger corpus)
        {
            // Deliberately not guarded per-file: a corpus tagger either produces
            // a coherent answer for the whole set or it fails the tagger.
            var all = await corpus.DeriveAllAsync(new Corpus { Files = contexts }, ct).ConfigureAwait(false);
            foreach (var (id, tags) in all)
            {
                derived[id] = tags;
            }

            return (derived, 0);
        }

        if (tagger is not IFileTagger fileTagger)
        {
            return (derived, 0);
        }

        var failed = 0;
        foreach (var context in contexts)
        {
            try
            {
                derived[context.File.FileId] = await fileTagger.DeriveAsync(context, ct).ConfigureAwait(false);
            }
            catch (Exception ex) when (ex is not OperationCanceledException)
            {
                // One unparseable file should not stop the other 91,237.
                failed++;
                AddWarning(warnings, $"file {context.File.FileId}: {ex.GetType().Name}: {ex.Message}");
            }
        }

        return (derived, failed);
    }

    private async Task<(int NeedingUpdate, int Pushed, int Failed)> PushAsync(
        ITagger tagger,
        TaggerRunOptions options,
        string serviceKey,
        Dictionary<int, FileRef> files,
        Dictionary<int, TagSet> candidates,
        ConcurrentDictionary<int, TaggerFileOutcome> outcomes,
        List<string> warnings,
        CancellationToken ct)
    {
        if (candidates.Count == 0)
        {
            return (0, 0, 0);
        }

        var pushedHashes = await _store
            .GetPushedHashesAsync(tagger.Id, candidates.Keys, ct)
            .ConfigureAwait(false);

        // Group by tag set, not by file: Hydrus applies one tag list to many
        // hashes per call, so identical sets collapse into a single request.
        // The Python pushed one HTTP call per file.
        var groups = new Dictionary<string, (TagSet Tags, List<FileRef> Files)>(StringComparer.Ordinal);
        var needingUpdate = 0;

        foreach (var (fileId, tags) in candidates)
        {
            if (tags.IsEmpty || pushedHashes.GetValueOrDefault(fileId) == tags.Hash)
            {
                continue;
            }

            if (!files.TryGetValue(fileId, out var file))
            {
                AddWarning(warnings, $"file {fileId}: no known hash; cannot push");
                continue;
            }

            needingUpdate++;
            if (!groups.TryGetValue(tags.Hash, out var group))
            {
                group = (tags, []);
                groups[tags.Hash] = group;
            }

            group.Files.Add(file);
        }

        _logger.LogInformation(
            "Tagger {TaggerId}: {Count} files need tag updates, in {Groups} distinct tag sets",
            tagger.Id, needingUpdate, groups.Count);

        if (options.DryRun)
        {
            return (needingUpdate, 0, 0);
        }

        var pushed = 0;
        var failed = 0;

        foreach (var (_, group) in groups)
        {
            foreach (var batch in group.Files.Chunk(options.PushBatchSize))
            {
                try
                {
                    await _hydrus
                        .AddTagsAsync([.. batch.Select(f => f.Hash)], serviceKey, group.Tags.Tags, ct)
                        .ConfigureAwait(false);

                    foreach (var file in batch)
                    {
                        var existing = outcomes.GetValueOrDefault(file.FileId)
                            ?? new TaggerFileOutcome(file.FileId);
                        outcomes[file.FileId] = existing with { PushedHash = group.Tags.Hash };
                        pushed++;
                    }
                }
                catch (Exception ex) when (ex is not OperationCanceledException)
                {
                    // No PushedHash recorded, so the next run retries this batch.
                    failed += batch.Length;
                    AddWarning(warnings, $"push of {batch.Length} files failed: {ex.GetType().Name}: {ex.Message}");
                }
            }
        }

        return (needingUpdate, pushed, failed);
    }

    private static void AddWarnings(List<string> warnings, IEnumerable<string> messages)
    {
        foreach (var message in messages)
        {
            AddWarning(warnings, message);
        }
    }

    /// <summary>Cap the list so a systemic failure cannot bury the report.</summary>
    private static void AddWarning(List<string> warnings, string message)
    {
        if (warnings.Count < MaxWarningsPerTagger)
        {
            warnings.Add(message);
        }
        else if (warnings.Count == MaxWarningsPerTagger)
        {
            warnings.Add($"... further warnings suppressed (more than {MaxWarningsPerTagger})");
        }
    }
}
