package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/storage"
	"github.com/jackc/pgx/v5"
)

const defaultMigrationLimit = 100

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Printf("media migration failed: %v", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("media-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apply := flags.Bool("apply", false, "copy verified objects and atomically switch media_assets to S3")
	limit := flags.Int("limit", defaultMigrationLimit, "maximum assets to inspect in this run (1-10000)")
	assetID := flags.String("asset-id", "", "migrate one media asset by ID")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("media-migrate does not accept positional arguments")
	}

	config, err := storage.ConfigFromEnv()
	if err != nil {
		return fmt.Errorf("media storage configuration: %w", err)
	}
	if config.Backend != storage.BackendS3 {
		return errors.New("no changes made: media migration requires MEDIA_STORAGE_DRIVER=s3; the current local deployment remains unchanged")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	requireSSL, err := parseDatabaseSSL(os.Getenv("DATABASE_SSL"))
	if err != nil {
		return err
	}
	database, err := db.Connect(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), requireSSL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer database.Close()

	source, err := newFilesystemSource(config.LocalDir)
	if err != nil {
		return fmt.Errorf("open legacy local storage: %w", err)
	}
	target, err := storage.NewS3(ctx, config)
	if err != nil {
		return fmt.Errorf("open S3 target: %w", err)
	}
	migration := migrator{
		repository: &postgresAssetRepository{database: database},
		source:     source,
		target:     target,
	}
	summary, migrationErr := migration.Run(ctx, migrationOptions{
		Apply: *apply, Limit: *limit, AssetID: strings.TrimSpace(*assetID), Bucket: config.S3Bucket,
	})
	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Fprintf(stdout, "%s media migration summary: scanned=%d planned=%d copied=%d updated=%d already_applied=%d failed=%d bytes=%d\n",
		mode, summary.Scanned, summary.Planned, summary.Copied, summary.Updated,
		summary.AlreadyApplied, summary.Failed, summary.Bytes)
	if !*apply {
		fmt.Fprintln(stdout, "Dry-run only: no S3 objects were written and no database rows were changed. Re-run with --apply to copy and publish verified objects.")
	}
	return migrationErr
}

type postgresAssetRepository struct {
	database *db.DB
}

func (repository *postgresAssetRepository) ListLegacyLocal(ctx context.Context, limit int, assetID string) ([]legacyAsset, error) {
	rows, err := repository.database.Query(ctx, `
		SELECT id, owner_principal_id, purpose, object_key, content_type,
		       expected_size_bytes, expected_checksum_sha256
		FROM media_assets
		WHERE storage_driver = 'local'
		  AND bucket IS NULL
		  AND legacy_public_url IS NOT NULL
		  AND state = 'ready'
		  AND deleted_at IS NULL
		  AND ($1::text = '' OR id = $1)
		ORDER BY created_at ASC, id ASC
		LIMIT $2`, assetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := make([]legacyAsset, 0)
	for rows.Next() {
		var asset legacyAsset
		if err := rows.Scan(
			&asset.ID, &asset.OwnerPrincipalID, &asset.Purpose, &asset.ObjectKey,
			&asset.ContentType, &asset.ExpectedSizeBytes, &asset.ExpectedChecksumSHA256,
		); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (repository *postgresAssetRepository) SwitchToS3(ctx context.Context, request switchAssetRequest) (switchAssetResult, error) {
	result, err := repository.database.Exec(ctx, `
		UPDATE media_assets
		SET storage_driver = 's3', bucket = $3, object_key = $4,
		    expected_size_bytes = COALESCE(expected_size_bytes, $5),
		    verified_size_bytes = $5,
		    expected_checksum_sha256 = COALESCE(expected_checksum_sha256, $6),
		    verified_checksum_sha256 = $6,
		    etag = NULLIF($7, ''), verified_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND storage_driver = 'local' AND bucket IS NULL
		  AND object_key = $2 AND legacy_public_url IS NOT NULL
		  AND state = 'ready' AND deleted_at IS NULL`,
		request.AssetID, request.SourceObjectKey, request.TargetBucket,
		request.TargetObjectKey, request.Size, request.SHA256, request.ETag)
	if err != nil {
		return switchAssetResult{}, err
	}
	if result.RowsAffected() == 1 {
		return switchAssetResult{Updated: true}, nil
	}
	var driver, bucket, key string
	var size *int64
	var checksum *string
	err = repository.database.QueryRow(ctx, `
		SELECT storage_driver, COALESCE(bucket, ''), object_key,
		       verified_size_bytes, verified_checksum_sha256
		FROM media_assets WHERE id = $1`, request.AssetID).Scan(&driver, &bucket, &key, &size, &checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return switchAssetResult{}, errMigrationConflict
	}
	if err != nil {
		return switchAssetResult{}, err
	}
	if driver == storage.BackendS3 && bucket == request.TargetBucket && key == request.TargetObjectKey &&
		size != nil && *size == request.Size && checksum != nil && strings.EqualFold(*checksum, request.SHA256) {
		return switchAssetResult{Already: true}, nil
	}
	return switchAssetResult{}, errMigrationConflict
}

type filesystemSource struct {
	root string
}

func newFilesystemSource(root string) (*filesystemSource, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("MEDIA_LOCAL_DIR or UPLOADS_DIR is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	return &filesystemSource{root: filepath.Clean(resolved)}, nil
}

func (source *filesystemSource) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := storage.ValidateObjectKey(key); err != nil {
		return nil, err
	}
	target := filepath.Join(source.root, filepath.FromSlash(key))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(source.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, storage.ErrInvalidKey
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("legacy media source is not a regular file")
	}
	return file, nil
}

func parseDatabaseSSL(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on", "require":
		return true, nil
	default:
		return false, errors.New("DATABASE_SSL must be a boolean value")
	}
}
