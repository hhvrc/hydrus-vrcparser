using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;

namespace HydrusTagger.Core.Data;

public static class DataServiceCollectionExtensions
{
    /// <summary>
    /// Build the connection string for a database file, matching the pragmas the
    /// legacy Python relied on.
    /// </summary>
    public static string BuildConnectionString(string databasePath) =>
        new SqliteConnectionStringBuilder
        {
            DataSource = databasePath,
            ForeignKeys = true,
            Mode = SqliteOpenMode.ReadWriteCreate,
            Cache = SqliteCacheMode.Private,
        }.ToString();

    /// <remarks>
    /// Registers a factory rather than only a scoped context: taggers extract
    /// several files concurrently, and a <see cref="TaggerDbContext"/> is not
    /// thread-safe. The scoped registration stays for the ordinary
    /// one-context-per-operation callers.
    /// </remarks>
    public static IServiceCollection AddTaggerDb(this IServiceCollection services, string databasePath)
    {
        services.AddDbContextFactory<TaggerDbContext>(options => options
            .UseSqlite(BuildConnectionString(databasePath))
            .AddInterceptors(new SqlitePragmaInterceptor()));

        services.AddScoped(sp => sp.GetRequiredService<IDbContextFactory<TaggerDbContext>>().CreateDbContext());

        return services;
    }
}
