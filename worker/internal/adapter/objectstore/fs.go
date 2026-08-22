// fs.go — backend de Store que guarda los objetos en una carpeta local, replicando
// la jerarquía de claves bajo StorageDir. Permite ejercitar el worker sin MinIO.
// Se implementa en el PASO 3.

package objectstore

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type FS struct {
    dir string
}

var _ Store = (*FS)(nil)

func NewFS(dir string) *FS {
    // Normalizar aquí es clave: resolve() compara prefijos contra s.dir, y
    // filepath.Join limpia sus argumentos (p. ej. quita "./"). Si s.dir se guarda
    // crudo ("./data/objects") y full sale limpio ("data/objects/..."), el prefijo
    // nunca coincide y TODA clave se rechaza. Con Clean ambos lados quedan iguales.
    return &FS{dir: filepath.Clean(dir)}
}

func (s *FS) resolve(key string) (string, error) {
    clean := filepath.Clean("/" + filepath.FromSlash(key)) // ancla en "/" para neutralizar ".."
    full := filepath.Join(s.dir, clean)
    if full != s.dir && !strings.HasPrefix(full, s.dir+string(os.PathSeparator)) {
        return "", fmt.Errorf("clave fuera del almacenamiento: %s", key)
    }
    return full, nil
}

func (s *FS) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
    path, err := s.resolve(key)
    if err != nil {
        return err
    }
    // 1. Crear la jerarquía de carpetas.
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return fmt.Errorf("creando carpetas para %s: %w", key, err)
    }
    // 2. Escribir en un temporal en la misma carpeta.
    tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
    if err != nil {
        return fmt.Errorf("temporal para %s: %w", key, err)
    }
    tmpName := tmp.Name()
    defer os.Remove(tmpName) // si algo falla, no dejar basura

    // 3. Copiar los bytes del lector al archivo.
    if _, err := io.Copy(tmp, r); err != nil {
        tmp.Close()
        return fmt.Errorf("escribiendo %s: %w", key, err)
    }
    if err := tmp.Close(); err != nil {
        return fmt.Errorf("cerrando %s: %w", key, err)
    }
    // 4. Renombrar: el archivo aparece completo o no aparece (atómico).
    if err := os.Rename(tmpName, path); err != nil {
        return fmt.Errorf("publicando %s: %w", key, err)
    }
    return nil
}

func (s *FS) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    path, err := s.resolve(key)
    if err != nil {
        return nil, err
    }
    f, err := os.Open(path)
    if err != nil {
        // Traducir "no existe" al centinela común: así el núcleo lo trata igual venga
        // del fs o del servicio http (que devuelve 404).
        if os.IsNotExist(err) {
            return nil, ErrNotFound
        }
        return nil, err
    }
    return f, nil
}
