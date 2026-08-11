namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// The common schema all three VRChat metadata formats normalize to. Port of
/// the dict produced by <c>db_logic.py:_normalize_meta</c>.
/// </summary>
/// <remarks>
/// String fields default to empty rather than null, matching the Python
/// normalizer, so tag building can test them uniformly with a trim-and-check.
/// </remarks>
public sealed class VrcMetadata
{
    public string RawText { get; set; } = "";

    /// <summary>"screenshotmanager", "lfs", "xmp", or null for VRCX JSON.</summary>
    public string? Type { get; set; }

    public int? Index { get; set; }

    /// <summary>xmp:CreatorTool, when the source was XMP.</summary>
    public string? CreatorTool { get; set; }

    public VrcAuthor Author { get; set; } = new();
    public VrcWorld World { get; set; } = new();
    public VrcPosition Position { get; set; } = new();

    /// <summary>Render quality.</summary>
    public int Rq { get; set; }

    public List<VrcPlayer> Players { get; set; } = [];

    /// <summary>Creation timestamp; only XMP carries one.</summary>
    public DateTimeOffset? Created { get; set; }

    /// <summary>Apps that created or edited the image, from XMP provenance.</summary>
    public List<string> EditorSoftware { get; set; } = [];
}

public sealed class VrcAuthor
{
    public string Id { get; set; } = "";
    public string DisplayName { get; set; } = "";
}

public sealed class VrcWorld
{
    public string Id { get; set; } = "";
    public string InstanceId { get; set; } = "";
    public string Name { get; set; } = "";
}

public sealed class VrcPosition
{
    public double X { get; set; }
    public double Y { get; set; }
    public double Z { get; set; }
}

public sealed class VrcPlayer
{
    public string Id { get; set; } = "";
    public string DisplayName { get; set; } = "";
    public VrcPosition Position { get; set; } = new();
}
