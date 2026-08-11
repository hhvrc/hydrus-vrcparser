using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Core.Data;

/// <summary>
/// Models the cache database. The baseline configuration here mirrors the
/// schema the legacy Python created (<c>db_logic.py:init_db</c>) column for
/// column, so the first EF migration is a no-op against the existing
/// <c>vrchat.db</c> and later migrations diff cleanly from it.
/// </summary>
public class TaggerDbContext : DbContext
{
    public TaggerDbContext(DbContextOptions<TaggerDbContext> options) : base(options)
    {
    }

    public DbSet<DataDir> DataDirs => Set<DataDir>();
    public DbSet<FileRecord> Files => Set<FileRecord>();
    public DbSet<ItxtChunk> ItxtChunks => Set<ItxtChunk>();
    public DbSet<HydrusMetaRecord> HydrusMeta => Set<HydrusMetaRecord>();
    public DbSet<TagMapping> TagMappings => Set<TagMapping>();
    public DbSet<HashTag> HashTags => Set<HashTag>();
    public DbSet<PushRecord> Pushes => Set<PushRecord>();
    public DbSet<LegacySchemaMigration> LegacySchemaMigrations => Set<LegacySchemaMigration>();

    protected override void OnModelCreating(ModelBuilder b)
    {
        var iso = new IsoTimestampConverter();
        var isoNullable = new NullableIsoTimestampConverter();

        b.Entity<DataDir>(e =>
        {
            e.ToTable("data_dirs");
            e.HasKey(x => x.Id);
            e.Property(x => x.Id).HasColumnName("id").ValueGeneratedOnAdd();
            e.Property(x => x.Path).HasColumnName("path").IsRequired();
            e.HasAlternateKey(x => x.Path);
        });

        b.Entity<FileRecord>(e =>
        {
            e.ToTable("files");
            e.HasKey(x => x.FileId);

            // Hydrus assigns file ids; we never generate them.
            e.Property(x => x.FileId).HasColumnName("file_id").ValueGeneratedNever();
            e.Property(x => x.Hash).HasColumnName("hash").IsRequired();
            e.Property(x => x.FileExt).HasColumnName("file_ext").IsRequired();
            e.Property(x => x.DataDirId).HasColumnName("data_dir_id");
            e.Property(x => x.CreatedAt).HasColumnName("created_at").HasConversion(iso).IsRequired();
            e.Property(x => x.ParsedAt).HasColumnName("parsed_at").HasConversion(isoNullable);
            e.Property(x => x.Size).HasColumnName("size");
            e.Property(x => x.FileParserVersion).HasColumnName("file_parser_version").HasDefaultValue(0);
            e.Property(x => x.DataParserVersion).HasColumnName("data_parser_version").HasDefaultValue(0);

            // The live schema has BOTH an inline UNIQUE on hash and a separate
            // non-unique idx_files_hash. Model both so migrations do not try to
            // "fix" the redundancy.
            e.HasAlternateKey(x => x.Hash);
            e.HasIndex(x => x.Hash).HasDatabaseName("idx_files_hash");
            e.HasIndex(x => x.DataDirId).HasDatabaseName("idx_files_data_dir_id");

            e.HasOne(x => x.DataDir)
                .WithMany(d => d.Files)
                .HasForeignKey(x => x.DataDirId)
                .OnDelete(DeleteBehavior.NoAction);
        });

        b.Entity<ItxtChunk>(e =>
        {
            e.ToTable("itxt_chunks");
            e.HasKey(x => new { x.FileId, x.Seq });
            e.Property(x => x.FileId).HasColumnName("file_id");
            e.Property(x => x.Seq).HasColumnName("seq");
            e.Property(x => x.Keyword).HasColumnName("keyword");
            e.Property(x => x.CompressionFlag).HasColumnName("compression_flag");
            e.Property(x => x.CompressionMethod).HasColumnName("compression_method");
            e.Property(x => x.LanguageTag).HasColumnName("language_tag");
            e.Property(x => x.TranslatedKeyword).HasColumnName("translated_keyword");
            e.Property(x => x.Text).HasColumnName("text");
            e.Property(x => x.ContentType).HasColumnName("content_type")
                .IsRequired().HasDefaultValue("text");

            e.HasOne(x => x.File)
                .WithMany(f => f.ItxtChunks)
                .HasForeignKey(x => x.FileId)
                .OnDelete(DeleteBehavior.NoAction);
        });

        b.Entity<HydrusMetaRecord>(e =>
        {
            e.ToTable("hydrus_meta");
            e.HasKey(x => x.FileId);
            e.Property(x => x.FileId).HasColumnName("file_id").ValueGeneratedNever();
            e.Property(x => x.Width).HasColumnName("width");
            e.Property(x => x.Height).HasColumnName("height");
            e.Property(x => x.HasTransparency).HasColumnName("has_transparency").HasDefaultValue(false);
            e.Property(x => x.HasHumanReadableEmbeddedMetadata)
                .HasColumnName("has_human_readable_embedded_metadata").HasDefaultValue(false);
            e.Property(x => x.UpdatedAt).HasColumnName("updated_at").HasConversion(iso).IsRequired();

            e.HasOne(x => x.File).WithOne()
                .HasForeignKey<HydrusMetaRecord>(x => x.FileId)
                .OnDelete(DeleteBehavior.NoAction);
        });

        b.Entity<TagMapping>(e =>
        {
            e.ToTable("tag_mappings");
            e.HasKey(x => new { x.Parent, x.Child });
            e.Property(x => x.Parent).HasColumnName("parent").IsRequired();
            e.Property(x => x.Child).HasColumnName("child").IsRequired();
        });

        b.Entity<HashTag>(e =>
        {
            e.ToTable("hash_tags");
            e.HasKey(x => new { x.FileId, x.Tag });
            e.Property(x => x.FileId).HasColumnName("file_id");
            e.Property(x => x.Tag).HasColumnName("tag").IsRequired();
            e.HasIndex(x => x.FileId).HasDatabaseName("idx_hash_tags_file_id");

            e.HasOne(x => x.File).WithMany()
                .HasForeignKey(x => x.FileId)
                .OnDelete(DeleteBehavior.NoAction);
        });

        b.Entity<PushRecord>(e =>
        {
            e.ToTable("pushes");
            e.HasKey(x => x.FileId);
            e.Property(x => x.FileId).HasColumnName("file_id").ValueGeneratedNever();
            e.Property(x => x.TagHash).HasColumnName("tag_hash").IsRequired();
            e.Property(x => x.FirstPushed).HasColumnName("first_pushed").HasConversion(iso).IsRequired();
            e.Property(x => x.LastPushed).HasColumnName("last_pushed").HasConversion(iso).IsRequired();

            e.HasOne(x => x.File).WithOne()
                .HasForeignKey<PushRecord>(x => x.FileId)
                .OnDelete(DeleteBehavior.NoAction);
        });

        b.Entity<LegacySchemaMigration>(e =>
        {
            e.ToTable("schema_migrations");
            e.HasKey(x => x.Id);
            e.Property(x => x.Id).HasColumnName("id").ValueGeneratedNever();
            e.Property(x => x.Name).HasColumnName("name").IsRequired();
            e.Property(x => x.AppliedAt).HasColumnName("applied_at").HasConversion(iso).IsRequired();
            e.HasAlternateKey(x => x.Name);
        });
    }
}
