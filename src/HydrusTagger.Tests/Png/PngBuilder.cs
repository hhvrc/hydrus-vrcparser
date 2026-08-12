using System.Buffers.Binary;
using System.Text;

namespace HydrusTagger.Tests.Png;

/// <summary>Builds minimal PNG bytes for tests. CRCs are zero; nothing verifies them.</summary>
internal static class PngBuilder
{
    /// <summary>
    /// An iTXt chunk payload:
    /// <c>keyword \0 compFlag compMethod language \0 translated \0 text</c>.
    /// </summary>
    public static byte[] Itxt(
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

    public static byte[] Png(params (string Type, byte[] Data)[] chunks)
    {
        var buffer = new List<byte>([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);
        var len = new byte[4];

        foreach (var (type, data) in chunks)
        {
            BinaryPrimitives.WriteUInt32BigEndian(len, (uint)data.Length);
            buffer.AddRange(len);
            buffer.AddRange(Encoding.ASCII.GetBytes(type));
            buffer.AddRange(data);
            buffer.AddRange(new byte[4]);
        }

        return [.. buffer];
    }
}
