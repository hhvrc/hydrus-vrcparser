using System.Buffers.Binary;
using System.Text;

namespace HydrusTagger.Core.Png;

/// <summary>One iTXt chunk. All fields null means the chunk was unparseable.</summary>
public sealed record PngITxtRecord(
    int Seq,
    string? Keyword,
    int? CompressionFlag,
    int? CompressionMethod,
    string? LanguageTag,
    string? TranslatedKeyword,
    string? Text)
{
    public static PngITxtRecord Unparseable(int seq) => new(seq, null, null, null, null, null, null);

    public bool IsUnparseable => Keyword is null && Text is null;
}

public sealed record PngITxtReadResult(
    IReadOnlyList<PngITxtRecord> Records,
    bool IoError,
    string? Error)
{
    public static PngITxtReadResult Failure(bool ioError, string error) => new([], ioError, error);
}

/// <summary>
/// Extracts iTXt chunks from PNG bytes. Port of <c>core/png_itxt.py</c>, minus
/// the content-type classification, which is a tagger concern and lives with
/// the VRChat tagger.
/// </summary>
public static class PngITxtReader
{
    private static ReadOnlySpan<byte> PngHeader => [0x89, (byte)'P', (byte)'N', (byte)'G', 0x0D, 0x0A, 0x1A, 0x0A];

    /// <summary>
    /// Guards against a corrupt length field asking us to allocate wildly. No
    /// legitimate iTXt chunk approaches this; the PNG spec caps chunks at 2^31-1.
    /// </summary>
    private const int MaxChunkLength = 64 * 1024 * 1024;

    public static PngITxtReadResult ReadFile(string path)
    {
        try
        {
            using var stream = new FileStream(
                path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite,
                bufferSize: 32 * 1024, FileOptions.SequentialScan);

            return Read(stream);
        }
        catch (Exception ex) when (ex is FileNotFoundException or DirectoryNotFoundException
                                       or UnauthorizedAccessException or IOException)
        {
            // Transient or environmental: the legacy pipeline retried these
            // rather than marking the file failed (png_itxt.py:163).
            return PngITxtReadResult.Failure(ioError: true, ex.Message);
        }
    }

    public static PngITxtReadResult Read(Stream stream)
    {
        var records = new List<PngITxtRecord>();

        try
        {
            Span<byte> signature = stackalloc byte[8];
            if (!TryReadExactly(stream, signature))
            {
                return PngITxtReadResult.Failure(ioError: false, "Not a valid PNG file");
            }

            if (!signature.SequenceEqual(PngHeader))
            {
                return PngITxtReadResult.Failure(ioError: false, "Not a valid PNG file");
            }

            Span<byte> header = stackalloc byte[8];
            var seq = 0;

            while (true)
            {
                if (!TryReadExactly(stream, header))
                {
                    break;
                }

                var size = BinaryPrimitives.ReadUInt32BigEndian(header[..4]);
                var type = header[4..8];

                if (type.SequenceEqual("IEND"u8))
                {
                    break;
                }

                if (type.SequenceEqual("iTXt"u8))
                {
                    if (size > MaxChunkLength)
                    {
                        return PngITxtReadResult.Failure(
                            ioError: false, $"iTXt chunk length {size} exceeds sanity limit");
                    }

                    var data = new byte[size];
                    if (!TryReadExactly(stream, data))
                    {
                        break;
                    }

                    Skip(stream, 4); // CRC
                    records.Add(ParseRecord(data, seq));
                    seq++;
                }
                else
                {
                    // Fast path: never pull image data into memory.
                    Skip(stream, (long)size + 4);
                }
            }
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
        {
            return new PngITxtReadResult(records, IoError: true, ex.Message);
        }

        return new PngITxtReadResult(records, IoError: false, Error: null);
    }

    /// <summary>
    /// Parse the iTXt payload:
    /// <c>keyword \0 comp_flag(1) comp_method(1) language_tag \0 translated_keyword \0 text</c>.
    /// </summary>
    /// <remarks>
    /// The two flag bytes are raw bytes, not NUL-terminated strings, so they
    /// must be read positionally rather than by splitting on NUL.
    /// Compressed iTXt (compression_flag == 1) is not decompressed -- the
    /// legacy parser did not either, and no VRChat writer produces it. Such a
    /// chunk yields its raw bytes as text.
    /// </remarks>
    internal static PngITxtRecord ParseRecord(ReadOnlySpan<byte> data, int seq)
    {
        var nul = data.IndexOf((byte)0);
        if (nul < 0 || nul + 2 >= data.Length)
        {
            return PngITxtRecord.Unparseable(seq);
        }

        var keyword = Decode(data[..nul]);
        var compressionFlag = data[nul + 1];
        var compressionMethod = data[nul + 2];

        var remainder = data[(nul + 3)..];

        // Split on NUL into at most 3 parts; fewer means a malformed chunk.
        var firstNul = remainder.IndexOf((byte)0);
        if (firstNul < 0)
        {
            return PngITxtRecord.Unparseable(seq);
        }

        var afterLang = remainder[(firstNul + 1)..];
        var secondNul = afterLang.IndexOf((byte)0);
        if (secondNul < 0)
        {
            return PngITxtRecord.Unparseable(seq);
        }

        var languageTag = Decode(remainder[..firstNul]);
        var translatedKeyword = Decode(afterLang[..secondNul]);
        var text = Decode(afterLang[(secondNul + 1)..]);

        return new PngITxtRecord(
            seq, keyword, compressionFlag, compressionMethod, languageTag, translatedKeyword, text);
    }

    /// <summary>UTF-8 with replacement characters, matching Python's <c>errors="replace"</c>.</summary>
    private static string Decode(ReadOnlySpan<byte> bytes) => Encoding.UTF8.GetString(bytes);

    private static bool TryReadExactly(Stream stream, Span<byte> buffer)
    {
        var read = 0;
        while (read < buffer.Length)
        {
            var n = stream.Read(buffer[read..]);
            if (n == 0)
            {
                return false;
            }

            read += n;
        }

        return true;
    }

    private static void Skip(Stream stream, long count)
    {
        if (stream.CanSeek)
        {
            stream.Seek(count, SeekOrigin.Current);
            return;
        }

        Span<byte> scratch = stackalloc byte[4096];
        while (count > 0)
        {
            var n = stream.Read(scratch[..(int)Math.Min(count, scratch.Length)]);
            if (n == 0)
            {
                return;
            }

            count -= n;
        }
    }
}
