using System.Globalization;
using Microsoft.EntityFrameworkCore.Storage.ValueConversion;

namespace HydrusTagger.Core.Data;

/// <summary>
/// Stores timestamps in exactly the format the legacy Python wrote, so the two
/// implementations can share the database during the transition.
/// </summary>
/// <remarks>
/// <c>core/utils.py:now_utc_iso</c> is
/// <c>datetime.now(timezone.utc).isoformat()</c>, which yields
/// <c>2026-06-27T00:55:12.345678+00:00</c> -- six fractional digits and a
/// numeric UTC offset. EF's default DateTimeOffset mapping uses a different
/// layout, so we convert explicitly rather than let the column drift into a
/// second format halfway through the table.
/// </remarks>
public sealed class IsoTimestampConverter : ValueConverter<DateTimeOffset, string>
{
    /// <summary>Python <c>isoformat()</c> layout for an aware UTC datetime.</summary>
    private const string WriteFormat = "yyyy-MM-ddTHH:mm:ss.ffffffzzz";

    public IsoTimestampConverter()
        : base(v => Write(v), v => Read(v))
    {
    }

    public static string Write(DateTimeOffset value) =>
        value.ToUniversalTime().ToString(WriteFormat, CultureInfo.InvariantCulture);

    public static DateTimeOffset Read(string value) =>
        DateTimeOffset.Parse(value, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind);
}

/// <summary>Nullable counterpart of <see cref="IsoTimestampConverter"/>.</summary>
public sealed class NullableIsoTimestampConverter : ValueConverter<DateTimeOffset?, string?>
{
    public NullableIsoTimestampConverter()
        : base(
            v => v == null ? null : IsoTimestampConverter.Write(v.Value),
            v => v == null ? null : IsoTimestampConverter.Read(v))
    {
    }
}
