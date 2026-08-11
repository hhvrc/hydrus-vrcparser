namespace HydrusTagger.Core.Data;

/// <summary>
/// A Hydrus data directory root. Files live at
/// <c>&lt;path&gt;/f&lt;hash[:2]&gt;/&lt;hash&gt;.&lt;ext&gt;</c>.
/// </summary>
public class DataDir
{
    public int Id { get; set; }
    public required string Path { get; set; }

    public ICollection<FileRecord> Files { get; } = [];
}

/// <summary>
/// A Hydrus file we have seen. <see cref="FileId"/> is assigned by Hydrus, not
/// by us, so it is never database-generated.
/// </summary>
public class FileRecord
{
    public int FileId { get; set; }
    public required string Hash { get; set; }
    public required string FileExt { get; set; }
    public int DataDirId { get; set; }
    public DateTimeOffset CreatedAt { get; set; }
    public DateTimeOffset? ParsedAt { get; set; }
    public long Size { get; set; }

    /// <summary>
    /// Legacy per-file extraction version. Superseded by
    /// <c>tagger_file_state</c> in the AddTaggerScope migration; retained here
    /// so the baseline model matches the database as it exists today.
    /// </summary>
    public int FileParserVersion { get; set; }

    /// <summary>Legacy per-file derive version. See <see cref="FileParserVersion"/>.</summary>
    public int DataParserVersion { get; set; }

    public DataDir? DataDir { get; set; }
    public ICollection<ItxtChunk> ItxtChunks { get; } = [];
}

/// <summary>
/// One raw iTXt chunk read out of a PNG. Caching these is what makes a
/// derive-version bump cheap: no disk I/O is needed to re-parse.
/// </summary>
public class ItxtChunk
{
    public int FileId { get; set; }
    public int Seq { get; set; }
    public string? Keyword { get; set; }
    public int? CompressionFlag { get; set; }
    public int? CompressionMethod { get; set; }
    public string? LanguageTag { get; set; }
    public string? TranslatedKeyword { get; set; }
    public string? Text { get; set; }

    /// <summary>One of <c>json</c>, <c>xml</c>, <c>line</c>, <c>text</c>.</summary>
    public required string ContentType { get; set; }

    public FileRecord? File { get; set; }
}

/// <summary>Cached subset of Hydrus's own metadata, to avoid refetching.</summary>
public class HydrusMetaRecord
{
    public int FileId { get; set; }
    public int? Width { get; set; }
    public int? Height { get; set; }
    public bool HasTransparency { get; set; }
    public bool HasHumanReadableEmbeddedMetadata { get; set; }
    public DateTimeOffset UpdatedAt { get; set; }

    public FileRecord? File { get; set; }
}

/// <summary>A parent -> child tag implication, pushed to Hydrus as a mapping.</summary>
public class TagMapping
{
    public required string Parent { get; set; }
    public required string Child { get; set; }
}

/// <summary>Computed tag for a file, cached so runs can be diffed offline.</summary>
public class HashTag
{
    public int FileId { get; set; }
    public required string Tag { get; set; }

    public FileRecord? File { get; set; }
}

/// <summary>
/// Change-detection ledger: the hash of the tag set last pushed for a file.
/// A run only calls add_tags when this hash differs.
/// </summary>
public class PushRecord
{
    public int FileId { get; set; }
    public required string TagHash { get; set; }
    public DateTimeOffset FirstPushed { get; set; }
    public DateTimeOffset LastPushed { get; set; }

    public FileRecord? File { get; set; }
}

/// <summary>
/// The legacy Python migration ledger. Modelled so the baseline EF model is a
/// faithful mirror of the live database; inert once EF owns the schema.
/// </summary>
public class LegacySchemaMigration
{
    public int Id { get; set; }
    public required string Name { get; set; }
    public DateTimeOffset AppliedAt { get; set; }
}
