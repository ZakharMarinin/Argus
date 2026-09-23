package view

import (
	"fmt"
	"hash/fnv"
	"io/fs"
)

// assetVersion - короткий отпечаток статики: имена и размеры файлов.
//
// Ассеты меняются только вместе с бинарником, поэтому их можно кэшировать
// надолго - но после деплоя браузер обязан увидеть новый адрес. Отпечаток
// считается один раз при разборе шаблонов и живёт в Templates, а не в
// переменной пакета: глобальное изменяемое состояние тут не нужно.
func assetVersion(src fs.FS) string {
	h := fnv.New32a()

	_ = fs.WalkDir(src, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Запись в хеш не может завершиться ошибкой.
		_, _ = fmt.Fprintf(h, "%s:%d;", path, info.Size())

		return nil
	})

	return fmt.Sprintf("%x", h.Sum32())
}
