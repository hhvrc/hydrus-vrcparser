using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Design;

namespace HydrusTagger.Core.Data;

/// <summary>
/// Design-time factory so <c>dotnet ef</c> can build the model without booting
/// the CLI host. Point it at a specific file with the
/// <c>HYDRUSTAGGER_DB</c> environment variable; the default is the repository's
/// <c>vrchat.db</c>, two levels up from this project.
/// </summary>
public sealed class TaggerDbContextFactory : IDesignTimeDbContextFactory<TaggerDbContext>
{
    public TaggerDbContext CreateDbContext(string[] args)
    {
        var path = Environment.GetEnvironmentVariable("HYDRUSTAGGER_DB")
                   ?? Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "vrchat.db");

        var options = new DbContextOptionsBuilder<TaggerDbContext>()
            .UseSqlite(DataServiceCollectionExtensions.BuildConnectionString(path))
            .AddInterceptors(new SqlitePragmaInterceptor())
            .Options;

        return new TaggerDbContext(options);
    }
}
