using HydrusTagger.Core.Tagging;

namespace HydrusTagger.Tests.Tagging.Fakes;

/// <summary>Base for the fakes: everything the host reads, made settable.</summary>
public abstract class FakeTaggerBase(string id) : ITagger
{
    public string Id { get; } = id;

    public int DeriveVersion { get; set; } = 1;

    public IReadOnlyList<string> SelectorQuery { get; set; } = ["system:filetype is png"];

    public IReadOnlyList<string> DependsOnIds { get; set; } = [];

    IReadOnlyList<string> ITagger.DependsOn => DependsOnIds;
}

/// <summary>
/// Per-file tagger whose output and failures are scripted by the test.
/// </summary>
public sealed class FakeFileTagger(string id) : FakeTaggerBase(id), IFileTagger
{
    /// <summary>Tags to return per file id. Files absent here get no tags.</summary>
    public Dictionary<int, string[]> TagsByFile { get; } = [];

    /// <summary>Default tags for files not listed in <see cref="TagsByFile"/>.</summary>
    public string[]? DefaultTags { get; set; }

    /// <summary>File ids for which <see cref="DeriveAsync"/> throws.</summary>
    public HashSet<int> ThrowOnFiles { get; } = [];

    /// <summary>Files the host asked this tagger to derive, in order.</summary>
    public List<int> DerivedFiles { get; } = [];

    /// <summary>Upstream tags the host supplied, per file.</summary>
    public Dictionary<int, IReadOnlyDictionary<string, TagSet>> SeenUpstream { get; } = [];

    public Task<TagSet> DeriveAsync(TaggerContext context, CancellationToken ct)
    {
        DerivedFiles.Add(context.File.FileId);
        SeenUpstream[context.File.FileId] = context.UpstreamTags;

        if (ThrowOnFiles.Contains(context.File.FileId))
        {
            throw new InvalidOperationException($"scripted failure for {context.File.FileId}");
        }

        var tags = TagsByFile.GetValueOrDefault(context.File.FileId) ?? DefaultTags ?? [];
        return Task.FromResult(new TagSet(tags));
    }
}

/// <summary>Tagger that both reads bytes off disk and derives tags.</summary>
public sealed class FakeExtractorTagger(string id) : FakeTaggerBase(id), IFileExtractor, IFileTagger
{
    public int ExtractVersion { get; set; } = 1;

    public HashSet<int> FailExtractOn { get; } = [];

    public HashSet<int> ThrowOnExtract { get; } = [];

    public List<int> ExtractedFiles { get; } = [];

    public List<int> DerivedFiles { get; } = [];

    public string[] DefaultTags { get; set; } = ["fake"];

    public Task<ExtractResult> ExtractAsync(FileRef file, CancellationToken ct)
    {
        lock (ExtractedFiles)
        {
            ExtractedFiles.Add(file.FileId);
        }

        if (ThrowOnExtract.Contains(file.FileId))
        {
            throw new IOException($"scripted read failure for {file.FileId}");
        }

        return Task.FromResult(
            FailExtractOn.Contains(file.FileId)
                ? ExtractResult.Failed("scripted extract failure")
                : ExtractResult.Ok);
    }

    public Task<TagSet> DeriveAsync(TaggerContext context, CancellationToken ct)
    {
        DerivedFiles.Add(context.File.FileId);
        return Task.FromResult(new TagSet(DefaultTags));
    }
}

/// <summary>Whole-corpus tagger, to check it is handed every discovered file.</summary>
public sealed class FakeCorpusTagger(string id) : FakeTaggerBase(id), ICorpusTagger
{
    public List<int> SeenFiles { get; } = [];

    public Func<Corpus, IReadOnlyDictionary<int, TagSet>>? Produce { get; set; }

    public Exception? ThrowOnDerive { get; set; }

    public Task<IReadOnlyDictionary<int, TagSet>> DeriveAllAsync(Corpus corpus, CancellationToken ct)
    {
        SeenFiles.AddRange(corpus.Files.Select(f => f.File.FileId));

        if (ThrowOnDerive is not null)
        {
            throw ThrowOnDerive;
        }

        IReadOnlyDictionary<int, TagSet> result = Produce?.Invoke(corpus)
            ?? corpus.Files.ToDictionary(f => f.File.FileId, _ => new TagSet(["corpus"]));

        return Task.FromResult(result);
    }
}
