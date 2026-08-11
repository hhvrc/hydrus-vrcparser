namespace HydrusTagger.Taggers.Correlation;

/// <summary>A proposed (id, name) pairing, with the evidence behind it.</summary>
public sealed record Suggestion(
    string UserId,
    string Name,
    int Overlap,
    int IdFiles,
    int NameFiles,
    double Jaccard,
    double RunnerUpJaccard);

public sealed class CorrelationResult
{
    public List<Suggestion> Suggestions { get; } = [];
    public List<(string UserId, string Name)> PairedPairs { get; } = [];
    public List<string> AmbiguousPairedIds { get; } = [];
    public List<Suggestion> Rejected { get; } = [];
    public List<(string UserId, int Files)> OrphanIds { get; } = [];
    public List<(string Name, int Files)> OrphanNames { get; } = [];
    public int FileCount { get; set; }
    public int IdCount { get; set; }
    public int NameCount { get; set; }
}

public sealed record CorrelationOptions
{
    /// <summary>Minimum files an id and name must share to be suggested.</summary>
    public int MinOverlap { get; init; } = 2;

    /// <summary>Minimum file-set Jaccard similarity to suggest a pair.</summary>
    public double MinJaccard { get; init; } = 0.5;

    /// <summary>Reject if the runner-up scores above this fraction of the winner.</summary>
    public double MaxRunnerUpRatio { get; init; } = 0.6;
}

/// <summary>
/// Infers which <c>vrchat-user-id</c> tags belong with which
/// <c>vrchat-user-name</c> tags. Port of <c>core/user_correlation.py</c>.
/// </summary>
/// <remarks>
/// VRChat metadata emits a player's id and displayName together, so a true pair
/// appears on nearly the same set of files. Pairs whose file sets are identical
/// are already trivially linked and need no inference; the interesting cases
/// are those that strongly overlap but diverged, usually because the name (or
/// id) failed to parse on some of that user's screenshots.
///
/// Pure: no Hydrus, no I/O.
/// </remarks>
public static class UserCorrelator
{
    public const string IdPrefix = "vrchat-user-id:";
    public const string NamePrefix = "vrchat-user-name:";

    /// <summary>
    /// Correlate ids with names from per-file tag sets, where each element is
    /// the ids and names present on one file with prefixes already stripped.
    /// </summary>
    /// <remarks>
    /// A pair is accepted only when it co-occurs at least
    /// <see cref="CorrelationOptions.MinOverlap"/> times, clears
    /// <see cref="CorrelationOptions.MinJaccard"/>, is a mutual best match, and
    /// beats its runner-up by a clear margin. Deliberately conservative: a
    /// wrong identity pairing is worse than a missed one.
    /// </remarks>
    public static CorrelationResult Correlate(
        IEnumerable<(IReadOnlySet<string> Ids, IReadOnlySet<string> Names)> files,
        CorrelationOptions? options = null)
    {
        options ??= new CorrelationOptions();

        // Insertion order is preserved throughout: tie-breaks depend on it, and
        // the Python original relied on dict ordering for the same reason.
        var idFiles = new OrderedCounter();
        var nameFiles = new OrderedCounter();
        var co = new Dictionary<string, OrderedCounter>(StringComparer.Ordinal);
        var nameCo = new Dictionary<string, OrderedCounter>(StringComparer.Ordinal);

        var fileCount = 0;
        foreach (var (ids, names) in files)
        {
            fileCount++;
            foreach (var i in ids)
            {
                idFiles.Increment(i);
            }

            foreach (var n in names)
            {
                nameFiles.Increment(n);
            }

            foreach (var i in ids)
            {
                var row = GetOrAdd(co, i);
                foreach (var n in names)
                {
                    row.Increment(n);
                    GetOrAdd(nameCo, n).Increment(i);
                }
            }
        }

        var result = new CorrelationResult
        {
            FileCount = fileCount,
            IdCount = idFiles.Count,
            NameCount = nameFiles.Count,
        };

        // 1) Already paired: an id whose file set is identical to a name's.
        //    Only accept when the match is exclusive 1:1 on both sides -- if two
        //    ids always appear together, co-occurrence cannot separate them.
        var lockedIds = new HashSet<string>(StringComparer.Ordinal);
        var lockedNames = new HashSet<string>(StringComparer.Ordinal);
        var perfectNamesOf = new List<(string Id, List<string> Names)>();
        var perfectIdsOf = new Dictionary<string, List<string>>(StringComparer.Ordinal);

        foreach (var i in idFiles.Keys)
        {
            var perfects = CoOf(co, i).Entries
                .Where(e => e.Count == idFiles[i] && nameFiles[e.Key] == e.Count)
                .Select(e => e.Key)
                .ToList();

            if (perfects.Count == 0)
            {
                continue;
            }

            perfectNamesOf.Add((i, perfects));
            foreach (var n in perfects)
            {
                if (!perfectIdsOf.TryGetValue(n, out var list))
                {
                    list = [];
                    perfectIdsOf[n] = list;
                }

                list.Add(i);
            }
        }

        foreach (var (i, names) in perfectNamesOf)
        {
            var exclusive = names.Count == 1 && perfectIdsOf[names[0]].Count == 1;
            if (exclusive)
            {
                result.PairedPairs.Add((i, names[0]));
                lockedIds.Add(i);
                lockedNames.Add(names[0]);
            }
            else
            {
                result.AmbiguousPairedIds.Add(i);
                lockedIds.Add(i);
                foreach (var n in names)
                {
                    lockedNames.Add(n);
                }
            }
        }

        // 2) Orphans: tags that never co-occur with the opposite kind at all.
        foreach (var i in idFiles.Keys)
        {
            if (CoOf(co, i).Count == 0)
            {
                result.OrphanIds.Add((i, idFiles[i]));
            }
        }

        foreach (var n in nameFiles.Keys)
        {
            if (CoOf(nameCo, n).Count == 0)
            {
                result.OrphanNames.Add((n, nameFiles[n]));
            }
        }

        // 3) Over the still-unpaired tags, keep only mutually-best,
        //    high-confidence matches.
        string? BestIdFor(string name)
        {
            string? bestId = null;
            var bestOverlap = 0;
            var bestJaccard = -1.0;

            foreach (var (id, overlap) in CoOf(nameCo, name).Entries)
            {
                if (lockedIds.Contains(id))
                {
                    continue;
                }

                var jac = Jaccard(overlap, idFiles[id], nameFiles[name]);
                if (jac > bestJaccard || (jac == bestJaccard && overlap > bestOverlap))
                {
                    bestId = id;
                    bestOverlap = overlap;
                    bestJaccard = jac;
                }
            }

            return bestId;
        }

        foreach (var i in idFiles.Keys)
        {
            if (lockedIds.Contains(i))
            {
                continue;
            }

            var candidates = CoOf(co, i).Entries
                .Where(e => !lockedNames.Contains(e.Key))
                .Select(e => (Name: e.Key, Overlap: e.Count, Jaccard: Jaccard(e.Count, idFiles[i], nameFiles[e.Key])))
                .OrderByDescending(c => c.Jaccard)
                .ThenByDescending(c => c.Overlap)
                .ToList();

            if (candidates.Count == 0)
            {
                continue;
            }

            var (name, overlap, jaccard) = candidates[0];
            var runnerUp = candidates.Count > 1 ? candidates[1].Jaccard : 0.0;

            var suggestion = new Suggestion(
                i, name, overlap, idFiles[i], nameFiles[name],
                Math.Round(jaccard, 4), Math.Round(runnerUp, 4));

            // Thresholds are applied to the unrounded values; the rounding above
            // is for presentation only.
            var accepted = overlap >= options.MinOverlap
                           && jaccard >= options.MinJaccard
                           && string.Equals(BestIdFor(name), i, StringComparison.Ordinal)
                           && runnerUp <= jaccard * options.MaxRunnerUpRatio;

            if (accepted)
            {
                result.Suggestions.Add(suggestion);
            }
            else
            {
                result.Rejected.Add(suggestion);
            }
        }

        SortByConfidence(result.Suggestions);
        SortByConfidence(result.Rejected);

        return result;
    }

    private static void SortByConfidence(List<Suggestion> items)
    {
        // Stable, matching Python's list.sort, so equal scores keep discovery order.
        var sorted = items
            .OrderByDescending(s => s.Jaccard)
            .ThenByDescending(s => s.Overlap)
            .ToList();

        items.Clear();
        items.AddRange(sorted);
    }

    private static double Jaccard(int overlap, int aFiles, int bFiles)
    {
        var union = aFiles + bFiles - overlap;
        return union == 0 ? 0.0 : (double)overlap / union;
    }

    private static OrderedCounter GetOrAdd(Dictionary<string, OrderedCounter> map, string key)
    {
        if (!map.TryGetValue(key, out var counter))
        {
            counter = new OrderedCounter();
            map[key] = counter;
        }

        return counter;
    }

    private static OrderedCounter CoOf(Dictionary<string, OrderedCounter> map, string key) =>
        map.TryGetValue(key, out var counter) ? counter : OrderedCounter.Empty;

    /// <summary>
    /// A counter that preserves first-seen key order, matching the Python
    /// <c>Counter</c>/<c>defaultdict</c> behaviour the algorithm's tie-breaking
    /// depends on.
    /// </summary>
    private sealed class OrderedCounter
    {
        public static readonly OrderedCounter Empty = new();

        private readonly Dictionary<string, int> _counts = new(StringComparer.Ordinal);
        private readonly List<string> _order = [];

        public int Count => _order.Count;

        public IReadOnlyList<string> Keys => _order;

        public IEnumerable<(string Key, int Count)> Entries
        {
            get
            {
                foreach (var key in _order)
                {
                    yield return (key, _counts[key]);
                }
            }
        }

        public int this[string key] => _counts.TryGetValue(key, out var v) ? v : 0;

        public void Increment(string key)
        {
            if (_counts.TryGetValue(key, out var current))
            {
                _counts[key] = current + 1;
            }
            else
            {
                _counts[key] = 1;
                _order.Add(key);
            }
        }
    }
}
