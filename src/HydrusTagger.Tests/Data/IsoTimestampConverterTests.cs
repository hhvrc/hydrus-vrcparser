using HydrusTagger.Core.Data;

namespace HydrusTagger.Tests.Data;

/// <summary>
/// The cache database is shared with the legacy Python during the transition,
/// so timestamps we write must be indistinguishable from the ones it wrote.
/// Every timestamp in the live vrchat.db is exactly 32 characters in the form
/// produced by <c>datetime.now(timezone.utc).isoformat()</c>.
/// </summary>
public class IsoTimestampConverterTests
{
    [Theory]
    [InlineData("2025-08-31T00:31:20.588103+00:00")]
    [InlineData("2026-02-15T17:32:13.398031+00:00")]
    [InlineData("2025-08-31T02:48:09.464699+00:00")]
    public void RoundTripsRealStoredValuesByteForByte(string stored)
    {
        var parsed = IsoTimestampConverter.Read(stored);
        Assert.Equal(stored, IsoTimestampConverter.Write(parsed));
    }

    [Fact]
    public void WritesThePythonIsoformatLayout()
    {
        var value = new DateTimeOffset(2026, 2, 15, 17, 32, 13, TimeSpan.Zero).AddTicks(3980310);
        Assert.Equal("2026-02-15T17:32:13.398031+00:00", IsoTimestampConverter.Write(value));
    }

    [Fact]
    public void WrittenValuesAreAlwaysTheSameLengthAsExistingRows()
    {
        // A shorter render (e.g. dropping trailing zero microseconds, which
        // Python does when microsecond==0) would break the single-format
        // property the live table currently has.
        var midnightExactly = new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero);
        Assert.Equal(32, IsoTimestampConverter.Write(midnightExactly).Length);
    }

    [Fact]
    public void NormalizesNonUtcOffsetsToUtc()
    {
        var oslo = new DateTimeOffset(2026, 6, 27, 2, 55, 12, TimeSpan.FromHours(2));
        Assert.Equal("2026-06-27T00:55:12.000000+00:00", IsoTimestampConverter.Write(oslo));
    }

    [Fact]
    public void ReadsTimestampsThatOmitMicroseconds()
    {
        // Python's isoformat() drops the fractional part entirely when it is
        // zero, so older rows may lack it.
        var parsed = IsoTimestampConverter.Read("2025-08-31T00:31:20+00:00");
        Assert.Equal(new DateTimeOffset(2025, 8, 31, 0, 31, 20, TimeSpan.Zero), parsed);
    }
}
