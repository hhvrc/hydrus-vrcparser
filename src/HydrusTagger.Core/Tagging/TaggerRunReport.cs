namespace HydrusTagger.Core.Tagging;

/// <summary>Host-level knobs. Every tagger gets these for free.</summary>
public sealed class TaggerRunOptions
{
    /// <summary>
    /// Run everything except the two irreversible parts: no <c>add_tags</c>
    /// call, and no state recorded. Extraction caches are still written -- they
    /// are a read-through cache of file bytes, so filling them changes no
    /// outcome and saves the real run a network round trip.
    /// </summary>
    public bool DryRun { get; init; }

    /// <summary>Run only these taggers (plus their dependencies). Empty means all.</summary>
    public IReadOnlyList<string> OnlyTaggers { get; init; } = [];

    /// <summary>Local tag service to push to. Resolved by name when null.</summary>
    public string? TagServiceName { get; init; }

    /// <summary>Files per <c>file_metadata</c> request.</summary>
    public int MetadataBatchSize { get; init; } = 256;

    /// <summary>Hashes per <c>add_tags</c> request.</summary>
    public int PushBatchSize { get; init; } = 100;

    /// <summary>Concurrent <see cref="IFileExtractor.ExtractAsync"/> calls.</summary>
    public int ExtractConcurrency { get; init; } = 8;
}

public enum TaggerStatus
{
    /// <summary>Ran to completion, though individual files may have failed.</summary>
    Completed,

    /// <summary>Threw at the tagger level; nothing was recorded for it.</summary>
    Failed,

    /// <summary>Not run because a tagger it depends on failed or was skipped.</summary>
    SkippedMissingDependency,

    /// <summary>Excluded by <see cref="TaggerRunOptions.OnlyTaggers"/>.</summary>
    Skipped,
}

/// <summary>What one tagger did during a run.</summary>
public sealed record TaggerResult
{
    public required string TaggerId { get; init; }

    public required TaggerStatus Status { get; init; }

    public int Discovered { get; init; }
    public int Extracted { get; init; }
    public int ExtractFailed { get; init; }
    public int Derived { get; init; }
    public int DeriveFailed { get; init; }

    /// <summary>Files whose tag hash differed from the ledger.</summary>
    public int NeedingUpdate { get; init; }

    public int Pushed { get; init; }
    public int PushFailed { get; init; }

    /// <summary>Set when <see cref="Status"/> is not <see cref="TaggerStatus.Completed"/>.</summary>
    public string? Error { get; init; }

    /// <summary>Per-file errors, capped so one systemic failure cannot flood the report.</summary>
    public IReadOnlyList<string> Warnings { get; init; } = [];
}

public sealed record TaggerRunReport(IReadOnlyList<TaggerResult> Results)
{
    public bool AnyFailed => Results.Any(r => r.Status == TaggerStatus.Failed);
}
