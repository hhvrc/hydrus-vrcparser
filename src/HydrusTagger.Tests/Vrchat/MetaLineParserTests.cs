using HydrusTagger.Taggers.Vrchat;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>Port of <c>tests/test_meta_line_parser.py</c>.</summary>
public class MetaLineParserTests
{
    [Fact]
    public void ParsesScreenshotmanagerBasics()
    {
        var result = MetaLineParser.Parse(
            "screenshotmanager|0|author:usr_abc,TestUser|world:wrld_123,inst1,MyWorld");

        Assert.Equal("screenshotmanager", result.Type);
        Assert.Equal(0, result.Index);
        Assert.Equal("usr_abc", result.Author.Id);
        Assert.Equal("TestUser", result.Author.DisplayName);
        Assert.Equal("wrld_123", result.World.Id);
        Assert.Equal("MyWorld", result.World.Name);
    }

    [Fact]
    public void ParsesLfsType()
    {
        var result = MetaLineParser.Parse("lfs|5|author:usr_xyz,User2");

        Assert.Equal("lfs", result.Type);
        Assert.Equal(5, result.Index);
    }

    [Fact]
    public void ParsesPosition()
    {
        var result = MetaLineParser.Parse("screenshotmanager|0|pos:1.5,2.0,3.5");

        Assert.Equal(1.5, result.Position.X);
        Assert.Equal(2.0, result.Position.Y);
        Assert.Equal(3.5, result.Position.Z);
    }

    [Fact]
    public void ParsesRenderQuality()
    {
        Assert.Equal(4, MetaLineParser.Parse("screenshotmanager|0|rq:4").Rq);
    }

    [Fact]
    public void ParsesPlayers()
    {
        var result = MetaLineParser.Parse(
            "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;usr_p2,4.0,5.0,6.0,Player2");

        Assert.Equal(2, result.Players.Count);
        Assert.Equal("usr_p1", result.Players[0].Id);
        Assert.Equal("Player1", result.Players[0].DisplayName);
        Assert.Equal(4.0, result.Players[1].Position.X);
        Assert.Equal(5.0, result.Players[1].Position.Y);
        Assert.Equal(6.0, result.Players[1].Position.Z);
    }

    [Theory]
    [InlineData("unknown|0")]
    [InlineData("screenshotmanager")]
    [InlineData("screenshotmanager|abc")]
    public void ThrowsOnUnrecoverableInput(string line)
    {
        Assert.Throws<MetaParseException>(() => MetaLineParser.Parse(line));
    }

    [Fact]
    public void KeepsTheRecordWhenTheAuthorFieldIsMalformed()
    {
        // Lenient by design: one bad field must not discard the whole record.
        var result = MetaLineParser.Parse("screenshotmanager|0|author:no_comma");

        Assert.Equal("", result.Author.Id);
        Assert.Equal("screenshotmanager", result.Type);
    }

    [Fact]
    public void KeepsTheRecordWhenPositionIsMalformed()
    {
        var result = MetaLineParser.Parse("screenshotmanager|0|pos:1.0,2.0");

        Assert.Equal(0.0, result.Position.X);
    }

    [Fact]
    public void KeepsTheRecordWhenRenderQualityIsMalformed()
    {
        Assert.Equal(0, MetaLineParser.Parse("screenshotmanager|0|rq:abc").Rq);
    }

    [Fact]
    public void KeepsTheRecordWhenWorldHasTooFewParts()
    {
        Assert.Equal("", MetaLineParser.Parse("screenshotmanager|0|world:wrld_abc,12345").World.Id);
    }

    [Fact]
    public void IgnoresUnknownKeys()
    {
        var result = MetaLineParser.Parse("screenshotmanager|0|future_field:some_value");
        Assert.Equal("screenshotmanager", result.Type);
    }

    [Fact]
    public void PrefixesInstanceIdWithTheWorldId()
    {
        var result = MetaLineParser.Parse("screenshotmanager|0|world:wrld_abc,12345,Test World");
        Assert.Equal("wrld_abc:12345", result.World.InstanceId);
    }

    [Fact]
    public void ParsesBareWorldSegmentWithoutThePrefix()
    {
        // Older screenshotmanager output omits the "world:" key entirely.
        var result = MetaLineParser.Parse("screenshotmanager|0|wrld_abc,12345,Test World");

        Assert.Equal("wrld_abc", result.World.Id);
        Assert.Equal("Test World", result.World.Name);
        Assert.Equal("wrld_abc:12345", result.World.InstanceId);
    }

    [Fact]
    public void SkipsMalformedPlayerEntriesAndKeepsTheRest()
    {
        var result = MetaLineParser.Parse(
            "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;bad_entry;usr_p2,4.0,5.0,6.0,Player2");

        Assert.Equal(2, result.Players.Count);
        Assert.Equal("usr_p1", result.Players[0].Id);
        Assert.Equal("usr_p2", result.Players[1].Id);
    }

    [Fact]
    public void SkipsPlayersWithNonNumericCoordinates()
    {
        var result = MetaLineParser.Parse(
            "screenshotmanager|0|players:usr_p1,x,y,z,Player1;usr_p2,1.0,2.0,3.0,Player2");

        Assert.Single(result.Players);
        Assert.Equal("usr_p2", result.Players[0].Id);
    }

    [Fact]
    public void ParsesTheExactShapeFoundInTheLiveDatabase()
    {
        // Taken verbatim from a content_type='line' row in vrchat.db: the world
        // segment is bare and the display name contains a space.
        var result = MetaLineParser.Parse(
            "screenshotmanager|0|author:usr_89459bcc-2790-4805-acd7-819e7c28618c,Max Cheetos"
            + "|wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f,60145,Crimson Moon");

        Assert.Equal("usr_89459bcc-2790-4805-acd7-819e7c28618c", result.Author.Id);
        Assert.Equal("Max Cheetos", result.Author.DisplayName);
        Assert.Equal("wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f", result.World.Id);
        Assert.Equal("Crimson Moon", result.World.Name);
        Assert.Equal("wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f:60145", result.World.InstanceId);
    }

    [Fact]
    public void KeepsCommasInsideAWorldName()
    {
        // World is split into at most 3 parts, so the name keeps its commas.
        var result = MetaLineParser.Parse("screenshotmanager|0|world:wrld_abc,1,A, B, and C");
        Assert.Equal("A, B, and C", result.World.Name);
    }

    [Fact]
    public void KeepsCommasInsideAPlayerDisplayName()
    {
        var result = MetaLineParser.Parse("screenshotmanager|0|players:usr_p1,1,2,3,Smith, Jr.");

        Assert.Single(result.Players);
        Assert.Equal("Smith, Jr.", result.Players[0].DisplayName);
    }
}
