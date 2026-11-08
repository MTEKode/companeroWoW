# CompaneroWoW

Compañero para **World of Warcraft: Forever** (la versión oficial de Blizzard, cliente moderno). Te ayuda a seguir misiones, subir de nivel con eficiencia, saber cuándo entrenar o subir profesiones y responder cualquier otra pregunta sobre el juego, siempre en castellano.

Tiene dos piezas:

- **Addon de Lua** (`CompaneroWoW/`): lee tu estado en el juego (zona, misiones, personaje, profesiones…) y lo entrega a la app mediante una franja de píxeles en vivo y el archivo SavedVariables (`/compa sync`).
- **App de escritorio en Go** (`app/`): lee el estado, responde con IA y muestra una ventana superpuesta que se abre con un atajo de teclado.

## Requisitos

- Windows 11.
- World of Warcraft: Forever en **modo ventana o sin bordes** (la franja de píxeles no se lee en pantalla completa exclusiva).
- Al menos un proveedor de IA. Elige uno o varios; se prueban en el orden de `providerOrder`:
  - **Antigravity CLI** (`agy`): instálalo con `irm https://antigravity.google/cli/install.ps1 | iex`, ejecuta `agy` una vez y elige tu cuenta personal de Google (Free, Pro o Ultra). Ver [Antigravity CLI](#antigravity-cli).
  - **Codex CLI**: `npm i -g @openai/codex` y luego inicia sesión con `codex login`.
  - **APIs compatibles con OpenAI** (NVIDIA, Z.ai…): pon la clave en `.env`. Ver [Claves de API y `.env`](#claves-de-api-y-env).
- Para compilar: Go 1.27 (sin CGO; no necesitas compilador C).

> **Gemini CLI ya no sirve con cuentas personales.** Google cerró el inicio de sesión con Google para Gemini CLI el 18 de junio de 2026 y pide migrar a Antigravity. La app ya no usa Gemini CLI; una configuración antigua con `"gemini"` en `providerOrder` se cambia sola por `"antigravity"` al arrancar.
>
> Tampoco se pueden usar las apps web o de escritorio de Gemini o ChatGPT como proveedor: no tienen API oficial y automatizarlas incumple sus condiciones de uso.

## Instalación del addon

1. Copia la carpeta `CompaneroWoW/` de este repositorio en:

   ```
   World of Warcraft\_forever_\Interface\AddOns\CompaneroWoW
   ```

2. Entra al juego, activa el addon en la lista de addons y usa modo ventana sin bordes.

## Compilar y ejecutar la app

Desde la raíz del repositorio:

```powershell
powershell -File app\build.ps1
.\dist\CompaneroWoW.exe
```

`build.ps1` genera `dist\CompaneroWoW.exe` (ejecutable de ventana, sin consola). Para comprobar el estado sin abrir la ventana:

```powershell
.\dist\CompaneroWoW.exe --diag --diag-out diag.json
```

El diagnóstico indica si WoW está detectado, si existe SavedVariables, qué proveedores de IA están disponibles y el puerto del servidor de herramientas.

Al arrancar, la app se queda en segundo plano y muestra la ventana. Cerrar la ventana con la × solo la oculta; para salir de verdad usa **Salir** en el menú contextual de la cabecera (clic derecho). Si intentas abrir una segunda copia, se trae a primer plano la que ya está en marcha.

## Atajo de teclado

Por defecto **Ctrl+Shift+Espacio** muestra u oculta la ventana (y le da el foco al campo de texto). Puedes cambiarlo con `hotkey` en `config.json`, por ejemplo `"Alt+F1"` o `"Ctrl+Alt+C"`. Modificadores: `Ctrl`, `Alt`, `Shift`, `Win`. Teclas: `A`-`Z`, `0`-`9`, `F1`-`F24`, `Space`, `Enter`, `Tab`, `Esc`. Si otra aplicación ya usa el atajo, la ventana lo indicará y puedes elegir otro.

## Ventana

El panel es una ventana nativa de Windows (sin navegador integrado): sin barra de título, con borde dorado y fondo oscuro semitransparente, para que parezca parte de la interfaz del juego. Siempre está encima de las demás ventanas y no aparece en la barra de tareas.

- **Cabecera**: arrástrala para mover el panel. Tiene tres botones: **Limpiar** (borra el historial), **–** (oculta el panel) y **×** (también lo oculta). Cerrar con Alt+F4 también solo lo oculta.
- **Menú contextual**: clic derecho en la cabecera para **Limpiar historial** o **Salir**. **Salir** cierra la aplicación de verdad.
- **Tamaño**: arrastra los bordes o la esquina para cambiarlo; la posición y el tamaño se recuerdan en `window.json`.
- Escribe tu pregunta y pulsa **Enter** (**Shift+Enter** para un salto de línea). **Escape** oculta el panel.
- **Incluir captura**: envía una captura de la ventana de WoW para preguntas sobre lo que ves en pantalla.
- **Enviar** hace lo mismo que Enter. Mientras la IA responde, la barra inferior muestra «Pensando…».
- La barra inferior muestra el estado: WoW conectado (con nivel y zona), franja de píxeles no encontrada, o la antigüedad de los datos de SavedVariables.
- Los consejos proactivos (al subir de nivel, completar objetivos, entregar una misión, cambiar de zona, cambiar una profesión o aceptar una misión) aparecen en la conversación como tarjetas destacadas con «💡 Consejo». Como mucho uno por minuto y nunca en combate. Si el panel está oculto, aparece sin robar el foco del juego.
- Los enlaces se abren en tu navegador (solo http y https).
- El DPI se respeta: el panel se escala en pantallas con 125 %, 150 % y otros valores.

## Configuración (`config.json`)

El archivo está en `%APPDATA%\CompaneroWoW\config.json`. Si no existe, la app lo crea con valores por defecto. Los campos que no escribas conservan su valor por defecto.

```json
{
  "wowPath": "C:\\Program Files (x86)\\World of Warcraft\\_forever_",
  "account": "",
  "hotkey": "Ctrl+Shift+Space",
  "language": "es",
  "providerOrder": ["antigravity", "codex", "api"],
  "antigravityCommand": "agy",
  "codexCommand": "codex",
  "apiProviders": [
    {
      "name": "nvidia",
      "baseUrl": "https://integrate.api.nvidia.com/v1",
      "apiKeyEnv": "NVIDIA_KEY",
      "model": "meta/llama-4-maverick-17b-128e-instruct"
    },
    {
      "name": "zai",
      "baseUrl": "https://api.z.ai/api/paas/v4",
      "apiKeyEnv": "ZAI_KEY",
      "model": "NOMBRE_DEL_MODELO"
    }
  ],
  "mcpPort": 47321,
  "proactive": true,
  "webSources": [
    "warcraft.wiki.gg",
    "www.warcrafttavern.com",
    "mobalytics.gg",
    "www.icy-veins.com",
    "forevermeta.me",
    "forevermastery.com"
  ],
  "pixelPollMs": 250,
  "aiTimeoutSec": 120
}
```

| Campo | Qué hace |
| --- | --- |
| `wowPath` | Carpeta de WoW Forever (`_forever_`). Sin ella, o si no existe, la app sigue funcionando y pide configurarla. |
| `account` | Cuenta de WTF para SavedVariables. Vacío = la cuenta con el archivo más reciente. |
| `hotkey` | Atajo global para mostrar u ocultar la ventana. |
| `language` | Idioma de la interfaz. Las respuestas son siempre en castellano. |
| `providerOrder` | Orden de los proveedores: `antigravity`, `codex`, `api`. `api` usa cada entrada de `apiProviders`, en orden. Un nombre desconocido (como el antiguo `gemini`) se omite. |
| `antigravityCommand` / `codexCommand` | Nombre o ruta de los comandos de las CLI. Para `antigravityCommand` la app también busca `%LOCALAPPDATA%\agy\bin\agy.exe`. |
| `apiProviders` | Endpoints compatibles con OpenAI: `name`, `baseUrl`, `model` y la clave en `apiKeyEnv` (nombre de la variable, recomendado) o en `apiKey` (clave literal, no recomendado). Ver [Claves de API y `.env`](#claves-de-api-y-env). |
| `mcpPort` | Puerto local (solo 127.0.0.1) del servidor de herramientas que usan Antigravity CLI y Codex CLI. |
| `proactive` | `true` activa los consejos proactivos. |
| `webSources` | Dominios permitidos para buscar y leer páginas sobre WoW: Forever. |
| `pixelPollMs` | Cada cuántos milisegundos se lee la franja de píxeles. |
| `aiTimeoutSec` | Tiempo máximo por proveedor antes de probar el siguiente. |

Ficheros de la app en `%APPDATA%\CompaneroWoW\`: `config.json`, `.env` (opcional, claves de API), `companero.db` (historial, base de conocimiento y caché web), `window.json` (posición y tamaño de la ventana), `companero.log` (se rota a 5 MB), y las carpetas de trabajo de las CLI: `agy\` (la skill, `.agents\mcp_config.json` y `antigravity-settings.json`) y `codex\` (solo `AGENTS.md`). La carpeta antigua `.agents\` de la raíz se borra al arrancar.

## Antigravity CLI

La app usa **Antigravity CLI** (`agy`) como proveedor principal. Funciona con cuentas personales de Google (Free, Pro o Ultra) y guarda sus credenciales en el Administrador de credenciales de Windows.

1. Instálalo: `irm https://antigravity.google/cli/install.ps1 | iex`
2. Ejecuta `agy` una vez en una terminal y elige la cuenta personal de Google con la que quieres usarlo.
3. Comprueba que la app lo ve: `.\dist\CompaneroWoW.exe --diag --diag-out diag.json` debe listar `antigravity` como disponible.

Cómo lo usa la app: cada pregunta es un proceso `agy -p` que se lanza y termina solo (no queda nada residente), con la carpeta de trabajo `%APPDATA%\CompaneroWoW\agy`, que solo contiene la skill, la configuración MCP y los ajustes de la llamada. Antigravity recibe las herramientas del servidor local (`companero`) mediante `.agents\mcp_config.json`, con un token aleatorio de la sesión en la cabecera `Authorization`; el servidor rechaza cualquier petición sin ese token. Sus permisos se fijan en `antigravity-settings.json`, que se pasa con `--settings` solo para esa llamada: el modelo puede usar únicamente las herramientas de la app y leer la carpeta de capturas (`screens`). Se deniegan los comandos de shell, las escrituras, la descarga de URL y la lectura de los archivos de la carpeta de datos (base de datos, registro, `config.json`, ventana, informes de evaluación), de los `.env` conocidos y de las carpetas `~\.ssh`, `~\.aws` y `~\.gemini`. Lo que queda fuera de la carpeta de trabajo necesita aprobación, que una llamada sin interfaz rechaza. Tu configuración global de Antigravity no se modifica. La app nunca usa `--dangerously-skip-permissions`.

Las capturas no se adjuntan como imagen en modo headless: si la pregunta lleva una captura, se guarda en `screens` y el texto indica su ruta, que agy lee con `read_file`. El modelo también puede pedir una captura de la ventana de WoW con `capturar_pantalla`. Esas imágenes salen del PC hacia Antigravity.

**Permisos de Codex CLI.** Codex corre en `%APPDATA%\CompaneroWoW\codex` con un sandbox de solo lectura y un perfil de permisos de archivos que concede lectura solo a los archivos del sistema que necesita el sandbox y a su carpeta de trabajo. Lo impone el sandbox de Codex en Windows, no la app: comprueba con una pregunta de prueba que Codex no lee un archivo de tu perfil de usuario antes de confiar en ello.

## Claves de API y `.env`

Las claves no van en `config.json`. Ponlas en un archivo `.env` con este formato (plantilla en `.env.example`):

```
NVIDIA_KEY=tu-clave-de-nvidia
```

La app busca cada variable en este orden: la clave literal `apiKey` de `config.json` (si la hay); la variable de entorno del sistema con ese nombre; y el primer archivo `.env` que la defina, buscado en:

1. `%APPDATA%\CompaneroWoW\.env`
2. la carpeta del ejecutable (`dist\.env` en desarrollo)
3. solo en desarrollo, si el ejecutable está en `<repo>\dist` y existe `<repo>\app\go.mod`: la raíz del repositorio (`<repo>\.env`)

La carpeta de trabajo actual ya no se busca: un `.env` que esté en otra carpeta no se usa.

Reglas del archivo: `CLAVE=valor` por línea; se admite `export` delante; las líneas que empiezan por `#` son comentarios; los valores pueden ir entre comillas simples o dobles; y se aceptan finales de línea de Windows (CRLF).

**Nunca subas `.env` a git.** El repositorio ya lo ignora (`.gitignore`), pero comprueba con `git check-ignore -v .env` que sigue ignorado. Si una clave se filtra, revócala en el panel del proveedor y crea otra.

**Claves no compatibles:** las claves del *Alibaba Coding Plan* no se pueden usar en esta app, porque sus condiciones de uso prohíben usarlas desde aplicaciones.

## Comandos del addon

- `/compa sync`: guarda en SavedVariables los datos detallados (misiones, personaje, profesiones, etc.). Hazlo tras cambios importantes; la app lo recuerda si los datos son antiguos.
- `/compa strip on|off`: activa o desactiva la franja de píxeles.
- `/compa estado`: muestra el estado actual del addon.

## Atajos de herramientas (MCP)

Antigravity CLI, Codex CLI y las APIs con herramientas usan estas herramientas locales (todo el texto en español):

| Herramienta | Qué devuelve |
| --- | --- |
| `estado_conexion` | Conexión con WoW y antigüedad de los datos. |
| `obtener_ubicacion` | Zona, subzona, coordenadas y mapa. |
| `obtener_personaje` | Ficha del personaje: nivel, xp, dinero, salud. |
| `obtener_misiones` | Registro de misiones con objetivos y datos conocidos. |
| `obtener_mision` | Una misión concreta por su identificador. |
| `obtener_objetivo` | Objetivo actual (NPC, nivel, hostil o no). |
| `obtener_inventario` | Bolsas y espacios libres. |
| `obtener_equipo` | Equipo puesto. |
| `obtener_grupo` | Grupo o banda. |
| `obtener_hechizos` | Libro de hechizos. |
| `obtener_talentos` | Talentos guardados por el addon. |
| `obtener_profesiones` | Profesiones aprendidas. |
| `obtener_reputaciones` | Reputaciones con facciones. |
| `obtener_dinero` | Dinero en oro, plata y cobre. |
| `buscar_conocimiento` | Busca en la base de conocimiento aprendida del propio juego. |
| `npcs_en_mapa` | NPC conocidos en un mapa. |
| `buscar_web` | Busca en las fuentes de WoW: Forever de `webSources`. |
| `leer_pagina` | Lee una página de esas fuentes. |
| `capturar_pantalla` | Captura de la ventana de WoW. |

## Privacidad

Todo se guarda en tu equipo: la base de datos, el registro, la configuración y las capturas. Durante una pregunta salen del PC estos datos, hacia el proveedor de IA que elijas:

- la pregunta;
- un resumen del contexto del jugador (zona, personaje, misión rastreada, profesiones, dinero y avisos);
- el historial reciente de la conversación (hasta 10 mensajes);
- la captura de pantalla, solo si la pides o la herramienta la pide;
- lo que el modelo lee con las herramientas (resultados de conocimiento y de búsqueda web, páginas de `webSources`).

A quién: con Antigravity CLI, a Google, con la cuenta de Antigravity que elegiste; con Codex CLI, a OpenAI; con tu API, al proveedor que configuraste. La búsqueda web solo envía la consulta (en inglés) a DuckDuckGo y a warcraft.wiki.gg, y lee solo páginas de `webSources`. La app no pasa las claves de API a Antigravity ni a Codex.

El texto de la web es contenido externo: el modelo lo trata como datos y no sigue las instrucciones que contenga. El servidor de herramientas escucha solo en `127.0.0.1` y exige el token de sesión.

## Limitaciones conocidas

- Los datos de combate son secretos en WoW: Forever, así que la app no conoce el estado de combate con precisión. Queda para la versión 2 (issue #1).
- La franja de píxeles necesita modo ventana o sin bordes. En pantalla completa exclusiva no se puede leer.
- Las coordenadas de SavedVariables y de la franja se muestran como porcentaje del mapa (0 a 100).
- La base de conocimiento se aprende de tus `/compa sync`: cuanto más juegues y sincronices, más sabe la app.
