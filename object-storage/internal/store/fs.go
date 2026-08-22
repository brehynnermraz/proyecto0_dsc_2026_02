package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// tmpPrefix marca los archivos temporales de una escritura en curso. List los
// ignora para no exponer objetos a medio escribir.
const tmpPrefix = ".tmp-"

func init() {
	// mime.TypeByExtension no trae markdown en todas las plataformas, y el bundle
	// es Markdown de principio a fin.
	_ = mime.AddExtensionType(".md", "text/markdown; charset=utf-8")
	_ = mime.AddExtensionType(".markdown", "text/markdown; charset=utf-8")
}

// FSStore guarda cada objeto como un archivo bajo una carpeta raíz, replicando la
// jerarquía de la clave: la clave "bundles/u1/b1/index.md" se convierte en
// <root>/bundles/u1/b1/index.md.
//
// En Compose la raíz debe ser un volumen nombrado. Si fuera el disco del
// contenedor, los objetos se perderían al reiniciarlo y se violaría la regla del
// §4 sobre no depender del disco efímero.
type FSStore struct {
	root string
}

var _ Store = (*FSStore)(nil)

func NewFS(root string) (*FSStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolviendo la raíz %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("creando la raíz %q: %w", abs, err)
	}
	return &FSStore{root: abs}, nil
}

// Put escribe el objeto de forma atómica: primero a un archivo temporal en la
// misma carpeta y después un rename. Sin eso, un proceso que muera a mitad de la
// escritura dejaría un archivo truncado que parece completo, y el bundle publicado
// sería silenciosamente inválido.
func (s *FSStore) Put(_ context.Context, key string, r io.Reader) (ObjectInfo, error) {
	p, err := s.resolve(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ObjectInfo{}, fmt.Errorf("creando %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, tmpPrefix+"*")
	if err != nil {
		return ObjectInfo{}, err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op si el rename ya tuvo éxito
	}()

	n, err := io.Copy(tmp, r)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("escribiendo %s: %w", key, err)
	}
	if err := tmp.Sync(); err != nil {
		return ObjectInfo{}, err
	}
	if err := tmp.Close(); err != nil {
		return ObjectInfo{}, err
	}
	if err := os.Rename(tmpName, p); err != nil {
		return ObjectInfo{}, fmt.Errorf("publicando %s: %w", key, err)
	}

	return ObjectInfo{
		Key:         key,
		Size:        n,
		ContentType: contentType(key),
		ModTime:     time.Now(),
	}, nil
}

func (s *FSStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	p, err := s.resolve(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return nil, ObjectInfo{}, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, ObjectInfo{}, err
	}
	// Una clave que apunta a un directorio no es un objeto: se trata como ausente.
	if fi.IsDir() {
		f.Close()
		return nil, ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return f, ObjectInfo{
		Key:         key,
		Size:        fi.Size(),
		ContentType: contentType(key),
		ModTime:     fi.ModTime(),
	}, nil
}

func (s *FSStore) Head(_ context.Context, key string) (ObjectInfo, error) {
	p, err := s.resolve(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return ObjectInfo{}, err
	}
	if fi.IsDir() {
		return ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return ObjectInfo{
		Key:         key,
		Size:        fi.Size(),
		ContentType: contentType(key),
		ModTime:     fi.ModTime(),
	}, nil
}

func (s *FSStore) Delete(_ context.Context, key string) error {
	p, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return err
	}
	return nil
}

// List recorre la raíz y devuelve los objetos cuya clave empieza por prefix. Es
// O(n) sobre el total de archivos; suficiente para el volumen del proyecto y lo
// que permite a la API enumerar un bundle para empaquetarlo sin volumen compartido.
func (s *FSStore) List(_ context.Context, prefix string) ([]ObjectInfo, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	var out []ObjectInfo
	err := filepath.WalkDir(s.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), tmpPrefix) {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, ObjectInfo{
			Key:         key,
			Size:        fi.Size(),
			ContentType: contentType(key),
			ModTime:     fi.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resolve traduce una clave a una ruta del sistema de archivos, rechazando todo lo
// que se salga de la raíz.
//
// La clave llega desde datos que originó un usuario al cargar un archivo. Una
// clave como "../../etc/passwd" no puede acabar abriendo ese archivo. Se RECHAZA
// en vez de normalizar: limpiar "../fuera.md" a "fuera.md" es seguro pero deja un
// archivo cuya ruta no coincide con la que dice la base de datos, un fallo
// silencioso en lugar de uno visible.
//
// La barra invertida también se rechaza, aunque en Linux sea válida en un nombre:
// ninguna de nuestras claves la lleva, y aceptarla haría que el mismo objeto se
// comportara distinto en el Windows de desarrollo y en el contenedor Linux.
func (s *FSStore) resolve(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: clave vacía", ErrInvalidKey)
	}
	if strings.ContainsRune(key, '\\') {
		return "", fmt.Errorf("%w: %q contiene una barra invertida", ErrInvalidKey, key)
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("%w: %q es absoluta", ErrInvalidKey, key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: %q tiene un segmento inválido (%q)", ErrInvalidKey, key, seg)
		}
	}

	p := filepath.Join(s.root, filepath.FromSlash(key))

	// Cinturón y tirantes: tras Join, comprobar que sigue dentro de la raíz.
	if !strings.HasPrefix(p, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q se sale de la raíz", ErrInvalidKey, key)
	}
	return p, nil
}

// validatePrefix aplica a un prefijo de listado las mismas defensas anti-traversal
// que resolve, pero admite el prefijo vacío (enumerar todo) y segmentos vacíos por
// una barra final.
func validatePrefix(prefix string) error {
	if strings.ContainsRune(prefix, '\\') {
		return fmt.Errorf("%w: el prefijo %q contiene una barra invertida", ErrInvalidKey, prefix)
	}
	if strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("%w: el prefijo %q es absoluto", ErrInvalidKey, prefix)
	}
	for _, seg := range strings.Split(prefix, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: el prefijo %q tiene un segmento inválido (%q)", ErrInvalidKey, prefix, seg)
		}
	}
	return nil
}

// contentType deduce el tipo por la extensión de la clave. El almacén guarda solo
// bytes; el tipo no se persiste, se infiere en cada lectura.
func contentType(key string) string {
	if ct := mime.TypeByExtension(path.Ext(key)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
