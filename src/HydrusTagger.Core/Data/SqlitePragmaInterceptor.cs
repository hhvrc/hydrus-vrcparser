using System.Data.Common;
using Microsoft.EntityFrameworkCore.Diagnostics;

namespace HydrusTagger.Core.Data;

/// <summary>
/// Applies the same connection pragmas the legacy Python set in
/// <c>db_logic.py:init_db</c>. WAL keeps readers unblocked while a long tagging
/// run writes; synchronous=NORMAL is the usual WAL pairing.
/// </summary>
/// <remarks>
/// <c>foreign_keys</c> is enabled via the connection string ("Foreign Keys=True")
/// rather than here, because Microsoft.Data.Sqlite applies it before EF opens
/// any transaction.
/// </remarks>
public sealed class SqlitePragmaInterceptor : DbConnectionInterceptor
{
    public override void ConnectionOpened(DbConnection connection, ConnectionEndEventData eventData)
    {
        Apply(connection);
        base.ConnectionOpened(connection, eventData);
    }

    public override async Task ConnectionOpenedAsync(
        DbConnection connection,
        ConnectionEndEventData eventData,
        CancellationToken cancellationToken = default)
    {
        await ApplyAsync(connection, cancellationToken).ConfigureAwait(false);
        await base.ConnectionOpenedAsync(connection, eventData, cancellationToken).ConfigureAwait(false);
    }

    private static void Apply(DbConnection connection)
    {
        using var cmd = connection.CreateCommand();
        cmd.CommandText = "PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;";
        cmd.ExecuteNonQuery();
    }

    private static async Task ApplyAsync(DbConnection connection, CancellationToken ct)
    {
        var cmd = connection.CreateCommand();
        await using (cmd.ConfigureAwait(false))
        {
            cmd.CommandText = "PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;";
            await cmd.ExecuteNonQueryAsync(ct).ConfigureAwait(false);
        }
    }
}
