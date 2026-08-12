namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Orders taggers so that every tagger runs after the ones it depends on.
/// </summary>
public static class TaggerGraph
{
    /// <summary>
    /// Topologically sort by <see cref="ITagger.DependsOn"/>.
    /// </summary>
    /// <remarks>
    /// Independent taggers come out in id order, so a run's shape is
    /// reproducible regardless of DI registration order.
    /// </remarks>
    /// <exception cref="TaggerGraphException">
    /// Duplicate ids, an unknown dependency, or a cycle.
    /// </exception>
    public static IReadOnlyList<ITagger> Sort(IEnumerable<ITagger> taggers)
    {
        ArgumentNullException.ThrowIfNull(taggers);

        var byId = new Dictionary<string, ITagger>(StringComparer.Ordinal);
        foreach (var tagger in taggers)
        {
            if (!byId.TryAdd(tagger.Id, tagger))
            {
                throw new TaggerGraphException(
                    $"Two taggers share the id '{tagger.Id}'. Ids scope database rows, so they must be unique.");
            }
        }

        foreach (var tagger in byId.Values)
        {
            foreach (var dependency in tagger.DependsOn)
            {
                if (!byId.ContainsKey(dependency))
                {
                    throw new TaggerGraphException(
                        $"Tagger '{tagger.Id}' depends on '{dependency}', which is not registered.");
                }
            }
        }

        var ordered = new List<ITagger>(byId.Count);
        var state = new Dictionary<string, Mark>(StringComparer.Ordinal);
        var path = new List<string>();

        foreach (var id in byId.Keys.OrderBy(k => k, StringComparer.Ordinal))
        {
            Visit(id, byId, state, path, ordered);
        }

        return ordered;
    }

    private static void Visit(
        string id,
        Dictionary<string, ITagger> byId,
        Dictionary<string, Mark> state,
        List<string> path,
        List<ITagger> ordered)
    {
        if (state.TryGetValue(id, out var mark))
        {
            if (mark == Mark.Done)
            {
                return;
            }

            // Re-entering a node still on the stack: report the cycle itself,
            // not just the fact that one exists.
            var start = path.IndexOf(id);
            var cycle = string.Join(" -> ", path[start..].Append(id));
            throw new TaggerGraphException($"Tagger dependency cycle: {cycle}");
        }

        state[id] = Mark.InProgress;
        path.Add(id);

        foreach (var dependency in byId[id].DependsOn.OrderBy(d => d, StringComparer.Ordinal))
        {
            Visit(dependency, byId, state, path, ordered);
        }

        path.RemoveAt(path.Count - 1);
        state[id] = Mark.Done;
        ordered.Add(byId[id]);
    }

    private enum Mark
    {
        InProgress,
        Done,
    }
}

public sealed class TaggerGraphException : Exception
{
    public TaggerGraphException(string message)
        : base(message)
    {
    }

    public TaggerGraphException(string message, Exception innerException)
        : base(message, innerException)
    {
    }

    public TaggerGraphException()
    {
    }
}
