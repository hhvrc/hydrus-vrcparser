using HydrusTagger.Core.Data;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// <see cref="IVrchatChunkStore"/> over the <c>itxt_chunks</c> table.
/// </summary>
/// <remarks>
/// Uses <see cref="IDbContextFactory{TContext}"/> because the host extracts
/// several files at once and a <see cref="DbContext"/> is not thread-safe.
/// Writes are serialized behind a semaphore: SQLite allows only one writer, and
/// the parallelism that matters here is the network read, not the local insert.
/// </remarks>
public sealed class EfVrchatChunkStore : IVrchatChunkStore, IDisposable
{
    private readonly IDbContextFactory<TaggerDbContext> _contexts;
    private readonly SemaphoreSlim _writeLock = new(1, 1);

    public EfVrchatChunkStore(IDbContextFactory<TaggerDbContext> contexts) => _contexts = contexts;

    public async Task<IReadOnlyList<VrcChunk>> GetChunksAsync(int fileId, CancellationToken ct)
    {
        await using var db = await _contexts.CreateDbContextAsync(ct).ConfigureAwait(false);

        return await db.ItxtChunks.AsNoTracking()
            .Where(c => c.FileId == fileId)
            .OrderBy(c => c.Seq)
            .Select(c => new VrcChunk(c.Keyword, c.Text, c.ContentType))
            .ToListAsync(ct)
            .ConfigureAwait(false);
    }

    public async Task ReplaceChunksAsync(int fileId, IReadOnlyList<VrcStoredChunk> chunks, CancellationToken ct)
    {
        ArgumentNullException.ThrowIfNull(chunks);

        await _writeLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            await using var db = await _contexts.CreateDbContextAsync(ct).ConfigureAwait(false);
            await using var tx = await db.Database.BeginTransactionAsync(ct).ConfigureAwait(false);

            await db.ItxtChunks.Where(c => c.FileId == fileId).ExecuteDeleteAsync(ct).ConfigureAwait(false);

            if (chunks.Count > 0)
            {
                db.ItxtChunks.AddRange(chunks.Select(c => new ItxtChunk
                {
                    FileId = fileId,
                    Seq = c.Seq,
                    Keyword = c.Keyword,
                    CompressionFlag = c.CompressionFlag,
                    CompressionMethod = c.CompressionMethod,
                    LanguageTag = c.LanguageTag,
                    TranslatedKeyword = c.TranslatedKeyword,
                    Text = c.Text,
                    ContentType = c.ContentType,
                }));

                await db.SaveChangesAsync(ct).ConfigureAwait(false);
            }

            await tx.CommitAsync(ct).ConfigureAwait(false);
        }
        finally
        {
            _writeLock.Release();
        }
    }

    public async Task<string?> GetDataDirectoryAsync(int fileId, CancellationToken ct)
    {
        await using var db = await _contexts.CreateDbContextAsync(ct).ConfigureAwait(false);

        return await db.Files.AsNoTracking()
            .Where(f => f.FileId == fileId)
            .Select(f => f.DataDir!.Path)
            .FirstOrDefaultAsync(ct)
            .ConfigureAwait(false);
    }

    public void Dispose() => _writeLock.Dispose();
}
