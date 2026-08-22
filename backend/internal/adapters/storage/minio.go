package storage

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"okfbundler/internal/ports"
)

type MinioStore struct {
	client *minio.Client
	bucket string
}

func NewMinioStore(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinioStore, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}

	return &MinioStore{client: client, bucket: bucket}, nil
}

var _ ports.ObjectStore = (*MinioStore)(nil)

func (s *MinioStore) PutOriginal(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *MinioStore) GetOriginal(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *MinioStore) PutBundleFile(ctx context.Context, bundleKey, relativePath string, r io.Reader, size int64) error {
	key := fmt.Sprintf("%s/%s", bundleKey, relativePath)
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{})
	return err
}

// GetBundleStream empaqueta en zip, sobre la marcha, todos los objetos bajo
// bundleKey. El listado se hace antes de devolver el reader para poder
// reportar "no existe" como un error real: si esperáramos a que fallara el
// primer Read, el handler HTTP ya habría comprometido un 200 con el cuerpo
// vacío (ver sección 5.2: descarga por flujo, sin materializar el paquete
// completo en memoria de la API — por eso el zip se arma en un goroutine
// escribiendo a un io.Pipe, no en un buffer).
func (s *MinioStore) GetBundleStream(ctx context.Context, bundleKey string) (io.ReadCloser, error) {
	keys, err := s.listBundleObjects(ctx, bundleKey)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("bundle no encontrado: %s", bundleKey)
	}

	pr, pw := io.Pipe()
	go func() {
		zw := zip.NewWriter(pw)
		var streamErr error
		for _, key := range keys {
			streamErr = s.copyObjectIntoZip(ctx, zw, key, strings.TrimPrefix(key, bundleKey+"/"))
			if streamErr != nil {
				break
			}
		}
		if closeErr := zw.Close(); streamErr == nil {
			streamErr = closeErr
		}
		pw.CloseWithError(streamErr)
	}()

	return pr, nil
}

func (s *MinioStore) copyObjectIntoZip(ctx context.Context, zw *zip.Writer, key, nameInZip string) error {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, obj)
	return err
}

func (s *MinioStore) DeleteOriginal(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// DeleteBundle borra todos los objetos bajo bundleKey (index.md, log.md, los
// conceptos...). No falla si el bundle ya no tiene objetos.
func (s *MinioStore) DeleteBundle(ctx context.Context, bundleKey string) error {
	keys, err := s.listBundleObjects(ctx, bundleKey)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (s *MinioStore) listBundleObjects(ctx context.Context, bundleKey string) ([]string, error) {
	var keys []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: bundleKey + "/", Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}
