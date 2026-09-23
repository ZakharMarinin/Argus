package backend

import "errors"

// ErrUnreachable оборачивает любую ошибку связи с бэкендом (сеть,
// таймаут, не-200 ответ) - usecase переводит её в entity.ErrNotReady.
var ErrUnreachable = errors.New("argus-backend недоступен")

// ErrNotFound - бэкенд ответил 404: usecase переводит её в entity.ErrNotFound.
var ErrNotFound = errors.New("не найдено")
