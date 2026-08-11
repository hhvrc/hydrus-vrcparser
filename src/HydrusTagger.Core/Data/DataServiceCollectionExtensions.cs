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

    public static IServiceCollection AddTaggerDb(this IServiceCollection services, string databasePath)
    {
        services.AddDbContext<TaggerDbContext>(options => options
            .UseSqlite(BuildConnectionString(databasePath))
            .AddInterceptors(new SqlitePragmaInterceptor()));

        return services;
    }
}
