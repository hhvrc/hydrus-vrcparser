using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace HydrusTagger.Core.Data.Migrations
{
    /// <inheritdoc />
    public partial class Baseline : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "data_dirs",
                columns: table => new
                {
                    id = table.Column<int>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    path = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_data_dirs", x => x.id);
                    table.UniqueConstraint("AK_data_dirs_path", x => x.path);
                });

            migrationBuilder.CreateTable(
                name: "schema_migrations",
                columns: table => new
                {
                    id = table.Column<int>(type: "INTEGER", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    applied_at = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_schema_migrations", x => x.id);
                    table.UniqueConstraint("AK_schema_migrations_name", x => x.name);
                });

            migrationBuilder.CreateTable(
                name: "tag_mappings",
                columns: table => new
                {
                    parent = table.Column<string>(type: "TEXT", nullable: false),
                    child = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_tag_mappings", x => new { x.parent, x.child });
                });

            migrationBuilder.CreateTable(
                name: "files",
                columns: table => new
                {
                    file_id = table.Column<int>(type: "INTEGER", nullable: false),
                    hash = table.Column<string>(type: "TEXT", nullable: false),
                    file_ext = table.Column<string>(type: "TEXT", nullable: false),
                    data_dir_id = table.Column<int>(type: "INTEGER", nullable: false),
                    created_at = table.Column<string>(type: "TEXT", nullable: false),
                    parsed_at = table.Column<string>(type: "TEXT", nullable: true),
                    size = table.Column<long>(type: "INTEGER", nullable: false),
                    file_parser_version = table.Column<int>(type: "INTEGER", nullable: false, defaultValue: 0),
                    data_parser_version = table.Column<int>(type: "INTEGER", nullable: false, defaultValue: 0)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_files", x => x.file_id);
                    table.UniqueConstraint("AK_files_hash", x => x.hash);
                    table.ForeignKey(
                        name: "FK_files_data_dirs_data_dir_id",
                        column: x => x.data_dir_id,
                        principalTable: "data_dirs",
                        principalColumn: "id");
                });

            migrationBuilder.CreateTable(
                name: "hash_tags",
                columns: table => new
                {
                    file_id = table.Column<int>(type: "INTEGER", nullable: false),
                    tag = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_hash_tags", x => new { x.file_id, x.tag });
                    table.ForeignKey(
                        name: "FK_hash_tags_files_file_id",
                        column: x => x.file_id,
                        principalTable: "files",
                        principalColumn: "file_id");
                });

            migrationBuilder.CreateTable(
                name: "hydrus_meta",
                columns: table => new
                {
                    file_id = table.Column<int>(type: "INTEGER", nullable: false),
                    width = table.Column<int>(type: "INTEGER", nullable: true),
                    height = table.Column<int>(type: "INTEGER", nullable: true),
                    has_transparency = table.Column<bool>(type: "INTEGER", nullable: false, defaultValue: false),
                    has_human_readable_embedded_metadata = table.Column<bool>(type: "INTEGER", nullable: false, defaultValue: false),
                    updated_at = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_hydrus_meta", x => x.file_id);
                    table.ForeignKey(
                        name: "FK_hydrus_meta_files_file_id",
                        column: x => x.file_id,
                        principalTable: "files",
                        principalColumn: "file_id");
                });

            migrationBuilder.CreateTable(
                name: "itxt_chunks",
                columns: table => new
                {
                    file_id = table.Column<int>(type: "INTEGER", nullable: false),
                    seq = table.Column<int>(type: "INTEGER", nullable: false),
                    keyword = table.Column<string>(type: "TEXT", nullable: true),
                    compression_flag = table.Column<int>(type: "INTEGER", nullable: true),
                    compression_method = table.Column<int>(type: "INTEGER", nullable: true),
                    language_tag = table.Column<string>(type: "TEXT", nullable: true),
                    translated_keyword = table.Column<string>(type: "TEXT", nullable: true),
                    text = table.Column<string>(type: "TEXT", nullable: true),
                    content_type = table.Column<string>(type: "TEXT", nullable: false, defaultValue: "text")
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_itxt_chunks", x => new { x.file_id, x.seq });
                    table.ForeignKey(
                        name: "FK_itxt_chunks_files_file_id",
                        column: x => x.file_id,
                        principalTable: "files",
                        principalColumn: "file_id");
                });

            migrationBuilder.CreateTable(
                name: "pushes",
                columns: table => new
                {
                    file_id = table.Column<int>(type: "INTEGER", nullable: false),
                    tag_hash = table.Column<string>(type: "TEXT", nullable: false),
                    first_pushed = table.Column<string>(type: "TEXT", nullable: false),
                    last_pushed = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_pushes", x => x.file_id);
                    table.ForeignKey(
                        name: "FK_pushes_files_file_id",
                        column: x => x.file_id,
                        principalTable: "files",
                        principalColumn: "file_id");
                });

            migrationBuilder.CreateIndex(
                name: "idx_files_data_dir_id",
                table: "files",
                column: "data_dir_id");

            migrationBuilder.CreateIndex(
                name: "idx_files_hash",
                table: "files",
                column: "hash");

            migrationBuilder.CreateIndex(
                name: "idx_hash_tags_file_id",
                table: "hash_tags",
                column: "file_id");
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "hash_tags");

            migrationBuilder.DropTable(
                name: "hydrus_meta");

            migrationBuilder.DropTable(
                name: "itxt_chunks");

            migrationBuilder.DropTable(
                name: "pushes");

            migrationBuilder.DropTable(
                name: "schema_migrations");

            migrationBuilder.DropTable(
                name: "tag_mappings");

            migrationBuilder.DropTable(
                name: "files");

            migrationBuilder.DropTable(
                name: "data_dirs");
        }
    }
}
