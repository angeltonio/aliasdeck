package web

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type language string

const (
	languageEnglish        language = "en"
	languageSpanish        language = "es"
	languageCookieName              = "aliasdeck_language"
	languageCookieLifetime          = 365 * 24 * time.Hour
)

type pageData struct {
	Lang        language
	CurrentPath string
	CSRFToken   string
}

var messages = map[language]map[string]string{
	languageEnglish: {
		"language.label": "Language", "language.en": "EN", "language.es": "ES",
		"brand.control_plane": "control plane", "nav.aliases": "Aliases", "nav.devices": "Devices", "nav.logout": "Log out",
		"footer":      "AliasDeck control plane",
		"login.title": "Log in", "login.subtitle": "Sign in with the operator account to manage aliases and devices.",
		"login.setup_available": "First-run setup is available. Open /setup directly from the server host, or use the one-time credential for remote setup.",
		"field.username":        "Username", "field.password": "Password", "field.confirm_password": "Confirm password", "login.submit": "Log in",
		"setup.title": "Set up AliasDeck", "setup.subtitle": "Create the operator account for this server.", "setup.submit": "Create operator account",
		"aliases.title": "Aliases", "aliases.subtitle": "Command definitions this server hands to devices on sync.", "aliases.new": "New alias",
		"aliases.name": "Name", "aliases.command": "Command", "aliases.description": "Description", "aliases.description_placeholder": "Show working tree status",
		"aliases.create": "Create alias", "aliases.all": "All aliases", "aliases.empty": "No aliases yet — create the first one above.",
		"aliases.delete": "Delete", "aliases.delete_confirm": "Delete alias %q?",
		"aliases.edit": "Edit", "aliases.save": "Save", "aliases.cancel": "Cancel",
		"nav.profiles":    "Groups",
		"devices.preview": "Preview",
		"preview.title":   "What %s receives", "preview.heading": "What this machine receives:",
		"preview.subtitle": "Every alias on this server, resolved against this device with the same rules the next sync applies. Nothing is written and no sync is triggered.",
		"preview.back":     "Back to devices", "preview.result": "Result", "preview.receives": "receives", "preview.excluded": "excluded",
		"preview.count": "This device receives %d of them.", "preview.no_aliases": "No aliases exist on this server yet.",
		"preview.miss_disabled": "the alias is disabled", "preview.miss_platform": "not targeted at %s",
		"preview.miss_shell": "not targeted at %s", "preview.miss_profile": "this device is in none of its groups",
		"preview.miss_device": "pinned to other devices",
		"error.render":        "that page could not be rendered",
		"aliases.groups":      "Groups", "aliases.platforms": "Platforms", "aliases.shells": "Shells",
		"aliases.targeting": "Who receives it", "aliases.targeting_hint": "Leave a row unchecked to reach every one of them.",
		"aliases.all_groups": "all groups", "aliases.all_platforms": "all platforms", "aliases.all_shells": "all shells",
		"aliases.no_groups_yet": "No groups exist yet.",
		"error.alias_targeting": "that targeting selection is not valid",
		"devices.groups":        "Groups", "devices.edit": "Edit", "devices.save": "Save", "devices.cancel": "Cancel",
		"devices.no_groups_yet": "No groups exist yet.",
		"error.device_form":     "the form could not be read", "error.device_update": "could not update that device: %s",
		"error.device_missing": "that device no longer exists", "error.device_name_required": "a device needs a name",
		"error.device_group_missing": "one of those groups no longer exists",
		"profiles.title":             "Groups", "profiles.subtitle": "Group devices by purpose, then aim an alias at the group instead of at each machine.",
		"profiles.new": "New group", "profiles.name": "Name", "profiles.name_placeholder": "Workstations",
		"profiles.description": "Description", "profiles.description_placeholder": "Laptops that get the everyday shortcuts",
		"profiles.create": "Create group", "profiles.all": "All groups", "profiles.empty": "No groups yet — create the first one above.",
		"profiles.edit": "Edit", "profiles.save": "Save", "profiles.cancel": "Cancel", "profiles.delete": "Delete",
		"profiles.delete_confirm": "Delete group %q? Aliases aimed at it stop reaching the devices in it, and those devices lose their membership.",
		"devices.title":           "Devices", "devices.subtitle": "Machines enrolled against this control plane.", "devices.add": "+ Add device",
		"devices.empty": "No devices enrolled yet. Use \"Add device\" to mint an enrollment token.", "devices.name": "Name", "devices.platform": "Platform",
		"devices.shell": "Shell", "devices.status": "Status", "devices.last_seen": "Last seen", "devices.last_sync": "Last sync",
		"devices.never": "never", "devices.synced": "synced", "devices.never_synced": "never synced",
		"time.utc_label": "Timestamps are shown in UTC.", "time.utc_title": "UTC timestamp. Converted to your local browser time when JavaScript is enabled.",
		"time.local_title": "Local time ({zone}). UTC: {utc}", "time.local_label": "Timestamps are shown in your local browser time ({zone}).",
		"time.local_zone_fallback":  "local time zone",
		"devices.status.not_synced": "Not synced", "devices.detail.not_synced": "This device has not completed a sync yet.",
		"devices.status.not_seen": "Not seen", "devices.detail.not_seen": "This device has not checked in yet.",
		"devices.status.stale": "Stale", "devices.detail.stale": "This device has not checked in for over 24 hours.",
		"devices.status.sync_overdue": "Sync overdue", "devices.detail.sync_overdue": "This device has not synced for over 24 hours.",
		"devices.status.delayed": "Delayed", "devices.detail.delayed": "This device was last seen or synced over 15 minutes ago.",
		"devices.status.recent": "Recent", "devices.detail.recent": "This device checked in and synced within the last 15 minutes.",
		"add.title": "Add a device", "add.subtitle": "Mint a single-use enrollment token, then run one setup command on the new machine.",
		"add.checklist_title": "What the setup command does", "add.step.init": "Initialize AliasDeck on this device.", "add.step.register": "Register the device with this server.",
		"add.step.sync": "Run the first alias sync.", "add.step.load": "Load the synced aliases into the current shell.", "add.mint_title": "1. Mint a token",
		"add.token_help": "The token is single-use and expires in 15 minutes.", "add.auto": "Sync aliases automatically",
		"add.auto_help": "Downloads alias changes in the background and keeps the device connection status up to date on macOS. Other platforms still complete setup without background startup.",
		"add.frequency": "Alias sync frequency", "add.frequency_help": "Shorter intervals use more requests and may use more battery.", "add.mint": "Mint enrollment token",
		"frequency.5s": "5 seconds", "frequency.30s": "30 seconds", "frequency.1m": "1 minute", "frequency.5m": "5 minutes",
		"enroll.run": "2. Run on the new machine", "enroll.expires": "This token expires at %s.", "enroll.copy": "Copy",
		"enroll.waiting": "Waiting for the new machine to enroll and complete its first sync…", "enroll.expired": "This enrollment token expired. Mint a new token to try again.",
		"enroll.enrolled": "Device enrolled. Waiting for its first sync…", "enroll.complete": "Device enrolled and synced. Redirecting…",
		"error.setup_form": "the setup form could not be read", "error.setup_link": "this setup link is invalid or has already been used",
		"error.setup_start": "could not start local setup", "error.setup_continue": "could not continue local setup",
		"error.password_mismatch": "passwords do not match", "error.password_weak": "password must be at least 12 characters", "error.operator_create": "could not create the operator account",
		"error.login_form": "the login form could not be read", "error.login_invalid": "invalid username or password", "error.login_busy": "too many password checks are already running; try again shortly", "error.session": "could not start a session, try again",
		"error.alias_form": "the form could not be read", "error.alias_create": "could not create that alias: %s", "error.alias_delete": "could not delete that alias",
		"error.alias_update": "could not update that alias: %s", "error.alias_missing": "that alias no longer exists",
		"error.profile_form": "the form could not be read", "error.profile_create": "could not create that group: %s",
		"error.profile_update": "could not update that group: %s", "error.profile_delete": "could not delete that group",
		"error.profile_conflict": "a group with that name already exists", "error.profile_missing": "that group no longer exists",
		"error.profile_load": "could not load groups", "error.profile_name_required": "a group needs a name",
		"error.alias_capacity": "this server already holds the maximum of 5000 aliases", "error.alias_conflict": "an alias with that name already exists",
		"error.alias_load": "could not load aliases", "error.device_load": "could not load devices", "error.enrollment_mint": "could not mint an enrollment token",
		"error.enrollment_status": "could not check enrollment status", "error.language": "invalid language selection", "error.csrf": "this form is stale or belongs to another session; reload the page and try again",
		"error.command_empty": "command is empty", "error.command_long": "command is longer than 4096 characters", "error.command_multiline": "command spans multiple lines; aliases are single-line, use a shell function instead",
		"error.command_control": "command contains a control character", "error.description_long": "description is longer than 256 characters", "error.description_control": "description contains a control character",
	},
	languageSpanish: {
		"language.label": "Idioma", "language.en": "EN", "language.es": "ES",
		"brand.control_plane": "panel de control", "nav.aliases": "Alias", "nav.devices": "Dispositivos", "nav.logout": "Cerrar sesión",
		"footer":      "Panel de control de AliasDeck",
		"login.title": "Iniciar sesión", "login.subtitle": "Inicia sesión con la cuenta de operador para gestionar alias y dispositivos.",
		"login.setup_available": "La configuración inicial está disponible. Abre /setup directamente desde el host del servidor o usa la credencial de un solo uso para la configuración remota.",
		"field.username":        "Nombre de usuario", "field.password": "Contraseña", "field.confirm_password": "Confirmar contraseña", "login.submit": "Iniciar sesión",
		"setup.title": "Configurar AliasDeck", "setup.subtitle": "Crea la cuenta de operador de este servidor.", "setup.submit": "Crear cuenta de operador",
		"aliases.title": "Alias", "aliases.subtitle": "Definiciones de comandos que este servidor entrega a los dispositivos al sincronizar.", "aliases.new": "Nuevo alias",
		"aliases.name": "Nombre", "aliases.command": "Comando", "aliases.description": "Descripción", "aliases.description_placeholder": "Mostrar el estado del árbol de trabajo",
		"aliases.create": "Crear alias", "aliases.all": "Todos los alias", "aliases.empty": "Aún no hay alias; crea el primero arriba.",
		"aliases.delete": "Eliminar", "aliases.delete_confirm": "¿Eliminar el alias %q?",
		"aliases.edit": "Editar", "aliases.save": "Guardar", "aliases.cancel": "Cancelar",
		"nav.profiles":    "Grupos",
		"devices.preview": "Ver qué recibe",
		"preview.title":   "Lo que recibe %s", "preview.heading": "Lo que recibe esta máquina:",
		"preview.subtitle": "Todos los alias de este servidor, resueltos contra este dispositivo con las mismas reglas que aplica la próxima sincronización. No se escribe nada ni se dispara ninguna sincronización.",
		"preview.back":     "Volver a dispositivos", "preview.result": "Resultado", "preview.receives": "lo recibe", "preview.excluded": "excluido",
		"preview.count": "Este dispositivo recibe %d de ellos.", "preview.no_aliases": "Todavía no hay alias en este servidor.",
		"preview.miss_disabled": "el alias está desactivado", "preview.miss_platform": "no está dirigido a %s",
		"preview.miss_shell": "no está dirigido a %s", "preview.miss_profile": "este dispositivo no está en ninguno de sus grupos",
		"preview.miss_device": "está fijado a otros dispositivos",
		"error.render":        "esa página no se pudo generar",
		"aliases.groups":      "Grupos", "aliases.platforms": "Plataformas", "aliases.shells": "Shells",
		"aliases.targeting": "Quién lo recibe", "aliases.targeting_hint": "Deja una fila sin marcar para llegar a todos.",
		"aliases.all_groups": "todos los grupos", "aliases.all_platforms": "todas las plataformas", "aliases.all_shells": "todas las shells",
		"aliases.no_groups_yet": "Todavía no existe ningún grupo.",
		"error.alias_targeting": "esa selección de destino no es válida",
		"devices.groups":        "Grupos", "devices.edit": "Editar", "devices.save": "Guardar", "devices.cancel": "Cancelar",
		"devices.no_groups_yet": "Todavía no existe ningún grupo.",
		"error.device_form":     "no se pudo leer el formulario", "error.device_update": "no se pudo actualizar ese dispositivo: %s",
		"error.device_missing": "ese dispositivo ya no existe", "error.device_name_required": "el dispositivo necesita un nombre",
		"error.device_group_missing": "uno de esos grupos ya no existe",
		"profiles.title":             "Grupos", "profiles.subtitle": "Agrupa dispositivos por propósito y apunta un alias al grupo en lugar de a cada máquina.",
		"profiles.new": "Nuevo grupo", "profiles.name": "Nombre", "profiles.name_placeholder": "Estaciones de trabajo",
		"profiles.description": "Descripción", "profiles.description_placeholder": "Portátiles que reciben los atajos de uso diario",
		"profiles.create": "Crear grupo", "profiles.all": "Todos los grupos", "profiles.empty": "Aún no hay grupos; crea el primero arriba.",
		"profiles.edit": "Editar", "profiles.save": "Guardar", "profiles.cancel": "Cancelar", "profiles.delete": "Eliminar",
		"profiles.delete_confirm": "¿Eliminar el grupo %q? Los alias apuntados a él dejan de llegar a los dispositivos que contiene, y esos dispositivos pierden su pertenencia.",
		"devices.title":           "Dispositivos", "devices.subtitle": "Equipos registrados en este panel de control.", "devices.add": "+ Añadir dispositivo",
		"devices.empty": "Aún no hay dispositivos registrados. Usa \"Añadir dispositivo\" para crear un token de registro.", "devices.name": "Nombre", "devices.platform": "Plataforma",
		"devices.shell": "Shell", "devices.status": "Estado", "devices.last_seen": "Visto por última vez", "devices.last_sync": "Última sincronización",
		"devices.never": "nunca", "devices.synced": "sincronizado", "devices.never_synced": "nunca sincronizado",
		"time.utc_label": "Las marcas de tiempo se muestran en UTC.", "time.utc_title": "Marca de tiempo UTC. Se convierte a la hora local del navegador cuando JavaScript está habilitado.",
		"time.local_title": "Hora local ({zone}). UTC: {utc}", "time.local_label": "Las marcas de tiempo se muestran en la hora local del navegador ({zone}).",
		"time.local_zone_fallback":  "zona horaria local",
		"devices.status.not_synced": "Sin sincronizar", "devices.detail.not_synced": "Este dispositivo aún no ha completado una sincronización.",
		"devices.status.not_seen": "No visto", "devices.detail.not_seen": "Este dispositivo aún no se ha conectado.",
		"devices.status.stale": "Inactivo", "devices.detail.stale": "Este dispositivo no se ha conectado durante más de 24 horas.",
		"devices.status.sync_overdue": "Sincronización pendiente", "devices.detail.sync_overdue": "Este dispositivo no se ha sincronizado durante más de 24 horas.",
		"devices.status.delayed": "Retrasado", "devices.detail.delayed": "Este dispositivo fue visto o sincronizado por última vez hace más de 15 minutos.",
		"devices.status.recent": "Reciente", "devices.detail.recent": "Este dispositivo se conectó y sincronizó durante los últimos 15 minutos.",
		"add.title": "Añadir un dispositivo", "add.subtitle": "Crea un token de registro de un solo uso y ejecuta un comando de configuración en el nuevo equipo.",
		"add.checklist_title": "Qué hace el comando de configuración", "add.step.init": "Inicializa AliasDeck en este dispositivo.", "add.step.register": "Registra el dispositivo en este servidor.",
		"add.step.sync": "Ejecuta la primera sincronización de alias.", "add.step.load": "Carga los alias sincronizados en el shell actual.", "add.mint_title": "1. Crear un token",
		"add.token_help": "El token es de un solo uso y caduca en 15 minutos.", "add.auto": "Sincronizar alias automáticamente",
		"add.auto_help": "Descarga los cambios de alias en segundo plano y mantiene actualizado el estado de conexión del dispositivo en macOS. En otras plataformas, la configuración se completa sin inicio automático.",
		"add.frequency": "Frecuencia de sincronización de alias", "add.frequency_help": "Los intervalos más cortos usan más solicitudes y pueden consumir más batería.", "add.mint": "Crear token de registro",
		"frequency.5s": "5 segundos", "frequency.30s": "30 segundos", "frequency.1m": "1 minuto", "frequency.5m": "5 minutos",
		"enroll.run": "2. Ejecutar en el nuevo equipo", "enroll.expires": "Este token caduca a las %s.", "enroll.copy": "Copiar",
		"enroll.waiting": "Esperando a que el nuevo equipo se registre y complete su primera sincronización…", "enroll.expired": "Este token de registro ha caducado. Crea uno nuevo para volver a intentarlo.",
		"enroll.enrolled": "Dispositivo registrado. Esperando su primera sincronización…", "enroll.complete": "Dispositivo registrado y sincronizado. Redirigiendo…",
		"error.setup_form": "no se pudo leer el formulario de configuración", "error.setup_link": "este enlace de configuración no es válido o ya se ha utilizado",
		"error.setup_start": "no se pudo iniciar la configuración local", "error.setup_continue": "no se pudo continuar la configuración local",
		"error.password_mismatch": "las contraseñas no coinciden", "error.password_weak": "la contraseña debe tener al menos 12 caracteres", "error.operator_create": "no se pudo crear la cuenta de operador",
		"error.login_form": "no se pudo leer el formulario de inicio de sesión", "error.login_invalid": "el nombre de usuario o la contraseña no son válidos", "error.login_busy": "ya hay demasiadas verificaciones de contraseña en curso; inténtalo de nuevo en unos instantes", "error.session": "no se pudo iniciar la sesión; inténtalo de nuevo",
		"error.alias_form": "no se pudo leer el formulario", "error.alias_create": "no se pudo crear ese alias: %s", "error.alias_delete": "no se pudo eliminar ese alias",
		"error.alias_update": "no se pudo actualizar ese alias: %s", "error.alias_missing": "ese alias ya no existe",
		"error.profile_form": "no se pudo leer el formulario", "error.profile_create": "no se pudo crear ese grupo: %s",
		"error.profile_update": "no se pudo actualizar ese grupo: %s", "error.profile_delete": "no se pudo eliminar ese grupo",
		"error.profile_conflict": "ya existe un grupo con ese nombre", "error.profile_missing": "ese grupo ya no existe",
		"error.profile_load": "no se pudieron cargar los grupos", "error.profile_name_required": "el grupo necesita un nombre",
		"error.alias_capacity": "este servidor ya contiene el máximo de 5000 alias", "error.alias_conflict": "ya existe un alias con ese nombre",
		"error.alias_load": "no se pudieron cargar los alias", "error.device_load": "no se pudieron cargar los dispositivos", "error.enrollment_mint": "no se pudo crear un token de registro",
		"error.enrollment_status": "no se pudo consultar el estado del registro", "error.language": "selección de idioma no válida", "error.csrf": "este formulario está desactualizado o pertenece a otra sesión; recarga la página e inténtalo de nuevo",
		"error.command_empty": "el comando está vacío", "error.command_long": "el comando tiene más de 4096 caracteres", "error.command_multiline": "el comando ocupa varias líneas; los alias son de una sola línea, usa una función de shell",
		"error.command_control": "el comando contiene un carácter de control", "error.description_long": "la descripción tiene más de 256 caracteres", "error.description_control": "la descripción contiene un carácter de control",
	},
}

func translate(lang language, key string) string {
	if catalog, ok := messages[lang]; ok {
		if value, ok := catalog[key]; ok {
			return value
		}
	}
	if value, ok := messages[languageEnglish][key]; ok {
		return value
	}
	return key
}

func parseLanguageTag(raw string) (language, bool) {
	tag := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]))
	base := strings.SplitN(tag, "-", 2)[0]
	switch base {
	case string(languageEnglish):
		return languageEnglish, true
	case string(languageSpanish):
		return languageSpanish, true
	default:
		return languageEnglish, false
	}
}

func requestLanguage(r *http.Request) language {
	if lang, ok := parseLanguageTag(r.URL.Query().Get("lang")); ok && r.URL.Query().Get("lang") != "" {
		return lang
	}
	if cookie, err := r.Cookie(languageCookieName); err == nil {
		if lang, ok := parseLanguageTag(cookie.Value); ok {
			return lang
		}
	}
	type preference struct {
		lang    language
		quality float64
		order   int
	}
	var preferences []preference
	for order, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		lang, ok := parseLanguageTag(part)
		if !ok {
			continue
		}
		quality := 1.0
		for _, parameter := range strings.Split(part, ";")[1:] {
			if name, value, found := strings.Cut(strings.TrimSpace(parameter), "="); found && name == "q" {
				if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 && parsed <= 1 {
					quality = parsed
				} else {
					quality = 0
				}
			}
		}
		if quality > 0 {
			preferences = append(preferences, preference{lang, quality, order})
		}
	}
	sort.SliceStable(preferences, func(i, j int) bool { return preferences[i].quality > preferences[j].quality })
	if len(preferences) > 0 {
		return preferences[0].lang
	}
	return languageEnglish
}

func pageDataFor(r *http.Request) pageData {
	data := pageData{Lang: requestLanguage(r), CurrentPath: returnTargetFor(r)}
	if subj, ok := subjectFromContext(r.Context()); ok {
		data.CSRFToken = subj.CSRFToken
	}
	return data
}

func returnTargetFor(r *http.Request) string {
	copy := *r.URL
	query := copy.Query()
	query.Del("lang")
	copy.RawQuery = query.Encode()
	return safeReturnTarget(copy.RequestURI())
}

func safeReturnTarget(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
		return "/"
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path == "" {
		return "/"
	}
	return u.RequestURI()
}

func (a *webapp) setLanguageCookie(w http.ResponseWriter, r *http.Request, lang language) {
	http.SetCookie(w, &http.Cookie{Name: languageCookieName, Value: string(lang), Path: "/", MaxAge: int(languageCookieLifetime.Seconds()), HttpOnly: true, Secure: a.isSecureRequest(r), SameSite: http.SameSiteLaxMode})
}

func (a *webapp) withLanguagePreference(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("lang")
		if raw != "" {
			if lang, ok := parseLanguageTag(raw); ok {
				a.setLanguageCookie(w, r, lang)
			}
		}
		next(w, r)
	}
}

func (a *webapp) handleLanguage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, translate(requestLanguage(r), "error.language"), http.StatusBadRequest)
		return
	}
	lang, ok := parseLanguageTag(r.FormValue("language"))
	if !ok || (r.FormValue("language") != "en" && r.FormValue("language") != "es") {
		http.Error(w, translate(requestLanguage(r), "error.language"), http.StatusBadRequest)
		return
	}
	a.setLanguageCookie(w, r, lang)
	http.Redirect(w, r, safeReturnTarget(r.FormValue("return")), http.StatusSeeOther)
}

func localizeValidationError(lang language, err error) string {
	message := err.Error()
	switch {
	case message == "command is empty":
		return translate(lang, "error.command_empty")
	case strings.HasPrefix(message, "command is longer than"):
		return translate(lang, "error.command_long")
	case strings.HasPrefix(message, "command spans multiple lines"):
		return translate(lang, "error.command_multiline")
	case strings.HasPrefix(message, "command contains a control character"):
		return translate(lang, "error.command_control")
	case strings.HasPrefix(message, "description is longer than"):
		return translate(lang, "error.description_long")
	case strings.HasPrefix(message, "description contains a control character"):
		return translate(lang, "error.description_control")
	default:
		return message
	}
}

func formatted(lang language, key string, values ...any) string {
	return fmt.Sprintf(translate(lang, key), values...)
}
