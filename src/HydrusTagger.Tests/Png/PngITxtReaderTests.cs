using System.Buffers.Binary;
using System.Text;
using HydrusTagger.Core.Png;
using HydrusTagger.Core.Text;

namespace HydrusTagger.Tests.Png;

/// <summary>
/// Port of the iTXt binary-parsing half of <c>tests/test_png_itxt.py</c>, plus
/// stream-level coverage the Python had no direct tests for.
/// </summary>
public class PngITxtReaderTests
{
    private static byte[] MakeItxt(
        string keyword, byte compFlag = 0, byte compMethod = 0,
        string lang = "", string trans = "", string text = "")
    {
        var buffer = new List<byte>();
        buffer.AddRange(Encoding.UTF8.GetBytes(keyword));
        buffer.Add(0);
        buffer.Add(compFlag);
        buffer.Add(compMethod);
        buffer.AddRange(Encoding.UTF8.GetBytes(lang));
        buffer.Add(0);
        buffer.AddRange(Encoding.UTF8.GetBytes(trans));
        buffer.Add(0);
        buffer.AddRange(Encoding.UTF8.GetBytes(text));
        return [.. buffer];
    }

    [Fact]
    public void ParsesAnUncompressedChunk()
    {
        var r = PngITxtReader.ParseRecord(MakeItxt("Description", text: "hello world"), 0);

        Assert.Equal("Description", r.Keyword);
        Assert.Equal(0, r.CompressionFlag);
        Assert.Equal(0, r.CompressionMethod);
        Assert.Equal("", r.LanguageTag);
        Assert.Equal("", r.TranslatedKeyword);
        Assert.Equal("hello world", r.Text);
    }

    [Fact]
    public void PreservesANonZeroCompressionFlag()
    {
        // Compressed iTXt is not inflated, but the flag must survive rather
        // than the chunk being silently dropped.
        var r = PngITxtReader.ParseRecord(MakeItxt("Description", compFlag: 1, text: "compressed data"), 0);

        Assert.Equal(1, r.CompressionFlag);
        Assert.Equal("compressed data", r.Text);
    }

    [Fact]
    public void ParsesLanguageAndTranslatedKeyword()
    {
        var r = PngITxtReader.ParseRecord(
            MakeItxt("Description", lang: "en", trans: "Desc", text: "content"), 0);

        Assert.Equal("en", r.LanguageTag);
        Assert.Equal("Desc", r.TranslatedKeyword);
        Assert.Equal("content", r.Text);
    }

    [Fact]
    public void KeepsNullBytesInsideTheTextSection()
    {
        // Only the first two NULs after the flags are separators; the rest
        // belong to the text.
        var r = PngITxtReader.ParseRecord(MakeItxt("Description", text: "before\0after"), 0);

        Assert.False(r.IsUnparseable);
        Assert.Equal("before\0after", r.Text);
    }

    [Theory]
    // Missing the compression flag and method bytes.
    [InlineData("Description\0")]
    // No NUL at all.
    [InlineData("just some bytes with no null")]
    public void ReportsUnparseableForMalformedPayloads(string raw)
    {
        var r = PngITxtReader.ParseRecord(Encoding.UTF8.GetBytes(raw), 0);
        Assert.True(r.IsUnparseable);
    }

    [Fact]
    public void ReportsUnparseableWhenTheTextSeparatorsAreMissing()
    {
        // keyword + flags + language, but no separators for translated/text.
        var r = PngITxtReader.ParseRecord("Description\0\0\0en"u8.ToArray(), 0);
        Assert.True(r.IsUnparseable);
    }

    [Fact]
    public void AcceptsAnEmptyKeyword()
    {
        var r = PngITxtReader.ParseRecord(MakeItxt("", text: "some text"), 0);

        Assert.Equal("", r.Keyword);
        Assert.Equal("some text", r.Text);
    }

    [Fact]
    public void ParsesRealisticJsonPayload()
    {
        const string json = """{"author":{"id":"usr_abc","displayName":"Test"}}""";
        var r = PngITxtReader.ParseRecord(MakeItxt("Description", text: json), 0);

        Assert.Equal(json, r.Text);
    }

    [Fact]
    public void DecodesInvalidUtf8WithReplacementCharacters()
    {
        // Matches Python's errors="replace" rather than throwing.
        byte[] payload = [(byte)'K', 0, 0, 0, 0, 0, 0xFF, 0xFE];
        var r = PngITxtReader.ParseRecord(payload, 0);

        Assert.Equal("K", r.Keyword);
        Assert.Equal("��", r.Text);
    }

    // ---- stream level ----

    private static byte[] BuildPng(params (string Type, byte[] Data)[] chunks)
    {
        var buffer = new List<byte>([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);
        var len = new byte[4];

        foreach (var (type, data) in chunks)
        {
            BinaryPrimitives.WriteUInt32BigEndian(len, (uint)data.Length);
            buffer.AddRange(len);
            buffer.AddRange(Encoding.ASCII.GetBytes(type));
            buffer.AddRange(data);
            buffer.AddRange(new byte[4]); // CRC, not verified
        }

        return [.. buffer];
    }

    [Fact]
    public void ReadsMultipleItxtChunksInOrderAndSkipsOthers()
    {
        var png = BuildPng(
            ("IHDR", new byte[13]),
            ("iTXt", MakeItxt("Description", text: "first")),
            ("IDAT", new byte[64]),
            ("iTXt", MakeItxt("XML:com.adobe.xmp", text: "<x/>")),
            ("IEND", []));

        var result = PngITxtReader.Read(new MemoryStream(png));

        Assert.False(result.IoError);
        Assert.Null(result.Error);
        Assert.Equal(2, result.Records.Count);
        Assert.Equal(0, result.Records[0].Seq);
        Assert.Equal("first", result.Records[0].Text);
        Assert.Equal(1, result.Records[1].Seq);
        Assert.Equal("XML:com.adobe.xmp", result.Records[1].Keyword);
    }

    [Fact]
    public void StopsAtIend()
    {
        var png = BuildPng(
            ("iTXt", MakeItxt("Description", text: "kept")),
            ("IEND", []),
            ("iTXt", MakeItxt("Description", text: "after the end")));

        var result = PngITxtReader.Read(new MemoryStream(png));

        Assert.Single(result.Records);
        Assert.Equal("kept", result.Records[0].Text);
    }

    [Fact]
    public void RejectsNonPngData()
    {
        var result = PngITxtReader.Read(new MemoryStream("this is not a png"u8.ToArray()));

        Assert.Empty(result.Records);
        Assert.False(result.IoError);
        Assert.Equal("Not a valid PNG file", result.Error);
    }

    [Fact]
    public void RejectsATruncatedSignature()
    {
        var result = PngITxtReader.Read(new MemoryStream([0x89, 0x50]));
        Assert.Equal("Not a valid PNG file", result.Error);
    }

    [Fact]
    public void ReturnsWhatItHasWhenTheFileIsTruncatedMidStream()
    {
        var png = BuildPng(("iTXt", MakeItxt("Description", text: "complete")));
        // Chop off the trailing chunk mid-header.
        var truncated = png[..^2];

        var result = PngITxtReader.Read(new MemoryStream(truncated));

        Assert.Single(result.Records);
        Assert.Equal("complete", result.Records[0].Text);
    }

    [Fact]
    public void ReportsAnIoErrorForAMissingFile()
    {
        var result = PngITxtReader.ReadFile(Path.Combine(Path.GetTempPath(), "definitely-not-here.png"));

        Assert.True(result.IoError);
        Assert.Empty(result.Records);
    }

    [Fact]
    public void RefusesAnAbsurdChunkLength()
    {
        // A corrupt length field must not become a huge allocation.
        var png = new List<byte>([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);
        png.AddRange([0x7F, 0xFF, 0xFF, 0xFF]);
        png.AddRange("iTXt"u8.ToArray());

        var result = PngITxtReader.Read(new MemoryStream([.. png]));

        Assert.False(result.IoError);
        Assert.Contains("sanity limit", result.Error, StringComparison.Ordinal);
    }
}

public class TextSanitizerTests
{
    [Theory]
    [InlineData(null, "")]
    [InlineData("", "")]
    [InlineData("  hello  ", "hello")]
    [InlineData("\0\0{\"a\":1}", "{\"a\":1}")]
    [InlineData("﻿<x/>", "<x/>")]
    [InlineData("\0﻿  spaced  ", "spaced")]
    public void SanitizesLeadingNulsBomAndWhitespace(string? input, string expected)
    {
        Assert.Equal(expected, TextSanitizer.SanitizeItxt(input));
    }

    [Fact]
    public void LeavesMisEncodedTextAlone()
    {
        // Real stored display names are double-encoded UTF-8. Those bytes are
        // part of the tag value; repairing them here would change every tag
        // derived from them and break parity.
        const string mojibake = "Nightâˆ—";
        Assert.Equal(mojibake, TextSanitizer.SanitizeItxt(mojibake));
    }
}
