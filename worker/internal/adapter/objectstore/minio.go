// minio.go — backend de Store contra MinIO (minio-go/v7). Crea el bucket de forma
// idempotente al arrancar y usa una llave de acceso con política restringida al
// bucket. Se implementa en el PASO 3 (semana 3).

package objectstore
