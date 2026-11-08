# CompaneroWoW

Compañero para **World of Warcraft: Forever** (la versión oficial de Blizzard, cliente moderno). Te ayuda a seguir misiones, subir de nivel con eficiencia, saber cuándo entrenar o subir profesiones y responder cualquier otra pregunta sobre el juego, siempre en castellano.

Tiene dos piezas:

- **Addon de Lua** (`CompaneroWoW/`): lee tu estado en el juego (zona, misiones, personaje, profesiones…) y lo entrega a la app mediante una franja de píxeles en vivo y el archivo SavedVariables (`/compa sync`).
- **App de escritorio en Go** (`app/`): lee el estado, responde con IA y muestra una ventana superpuesta que se abre con un atajo de teclado.

## Requisitos

- Windows 11.
- World of Warcraft: Forever en **modo ventana o sin bordes** (la franja de píxeles no se lee en pantalla completa exclusiva).
- **Microsoft Edge WebView2 Runtime** (en Windows 11 viene instalado; si no, la app te mostrará el enlace de descarga).
- Al menos un proveedor de IA. Elige uno o varios; se prueban en el orden de `providerOrder`:
  - **Gemini CLI**: `npm i -g @google/gemini-cli` y luego inicia sesión con `gemini`.
  - **Codex CLI**: `npm i -g @openai/codex` y luego inicia sesión con `codex`.
  - **APIs compatibles con OpenAI** (NVIDIA, Z.ai, Alibaba DashScope…): pon la clave en `apiProviders` de `config.json`.
- Para compilar: Go 1.27 (sin CGO; no necesitas compilador C).

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

Al arrancar, la app se queda en segundo plano y muestra la ventana. Cerrar la ventana con la X solo la oculta; para salir de verdad usa el botón **Salir**. Si intentas abrir una segunda copia, se trae a primer plano la que ya está en marcha.

## Atajo de teclado

Por defecto **Ctrl+Shift+Espacio** muestra u oculta la ventana (y le da el foco al campo de texto). Puedes cambiarlo con `hotkey` en `config.json`, por ejemplo `"Alt+F1"` o `"Ctrl+Alt+C"`. Modificadores: `Ctrl`, `Alt`, `Shift`, `Win`. Teclas: `A`-`Z`, `0`-`9`, `F1`-`F24`, `Space`, `Enter`, `Tab`, `Esc`. Si otra aplicación ya usa el atajo, la ventana lo indicará y puedes elegir otro.

## Ventana

- Escribe tu pregunta y pulsa **Enter** (**Shift+Enter** para un salto de línea).
- **Incluir captura**: envía una captura de la ventana de WoW para preguntas sobre lo que ves en pantalla.
- **Limpiar historial** borra los mensajes guardados. **Salir** cierra la aplicación.
- La barra inferior muestra el estado: WoW conectado, franja de píxeles no encontrada, o la antigüedad de los datos de SavedVariables.
- Los consejos proactivos (al subir de nivel, completar objetivos, entregar una misión, cambiar de zona, cambiar una profesión o aceptar una misión) aparecen como tarjetas destacadas. Como mucho uno por minuto y nunca en combate.
- Los enlaces se abren en tu navegador.

## Configuración (`config.json`)

El archivo está en `%APPDATA%\CompaneroWoW\config.json`. Si no existe, la app lo crea con valores por defecto. Los campos que no escribas conservan su valor por defecto.

```json
{
  "wowPath": "C:\\Program Files (x86)\\World of Warcraft\\_forever_",
  "account": "",
  "hotkey": "Ctrl+Shift+Space",
  "language": "es",
  "providerOrder": ["gemini", "codex", "api"],
  "geminiCommand": "gemini",
  "codexCommand": "codex",
  "apiProviders": [
    {
      "name": "nvidia",
      "baseUrl": "https://integrate.api.nvidia.com/v1",
      "apiKey": "TU_CLAVE_NVIDIA",
      "model": "NOMBRE_DEL_MODELO"
    },
    {
      "name": "zai",
      "baseUrl": "https://api.z.ai/api/paas/v4",
      "apiKey": "TU_CLAVE_ZAI",
      "model": "NOMBRE_DEL_MODELO"
    },
    {
      "name": "alibaba",
      "baseUrl": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
      "apiKey": "TU_CLAVE_DASHSCOPE",
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
| `providerOrder` | Orden de los proveedores: `gemini`, `codex`, `api`. `api` usa cada entrada de `apiProviders`, en orden. |
| `geminiCommand` / `codexCommand` | Nombre o ruta de los comandos de las CLI. |
| `apiProviders` | Endpoints compatibles con OpenAI: `name`, `baseUrl`, `apiKey`, `model`. Sustituye las claves y el modelo por los tuyos. |
| `mcpPort` | Puerto local (solo 127.0.0.1) del servidor de herramientas que usan Gemini CLI y Codex CLI. |
| `proactive` | `true` activa los consejos proactivos. |
| `webSources` | Dominios permitidos para buscar y leer páginas sobre WoW: Forever. |
| `pixelPollMs` | Cada cuántos milisegundos se lee la franja de píxeles. |
| `aiTimeoutSec` | Tiempo máximo por proveedor antes de probar el siguiente. |

Ficheros de la app en `%APPDATA%\CompaneroWoW\`: `config.json`, `companero.db` (historial, base de conocimiento y caché web), `window.json` (posición y tamaño de la ventana), `companero.log` (se rota a 5 MB).

## Comandos del addon

- `/compa sync`: guarda en SavedVariables los datos detallados (misiones, personaje, profesiones, etc.). Hazlo tras cambios importantes; la app lo recuerda si los datos son antiguos.
- `/compa strip on|off`: activa o desactiva la franja de píxeles.
- `/compa estado`: muestra el estado actual del addon.

## Atajos de herramientas (MCP)

Gemini CLI y Codex CLI, y las APIs con herramientas, usan estas herramientas locales (todo el texto en español):

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

Todo se guarda y se procesa en tu equipo. Lo único que sale de él es la pregunta (y, si la pides, la captura) hacia el proveedor de IA que elijas (Gemini CLI, Codex CLI o tu API), y las búsquedas o páginas que pide la herramienta de búsqueda a las fuentes de `webSources`. El servidor de herramientas escucha solo en `127.0.0.1`.

## Limitaciones conocidas

- Los datos de combate son secretos en WoW: Forever, así que la app no conoce el estado de combate con precisión. Queda para la versión 2 (issue #1).
- La franja de píxeles necesita modo ventana o sin bordes. En pantalla completa exclusiva no se puede leer.
- Las coordenadas de SavedVariables y de la franja se muestran como porcentaje del mapa (0 a 100).
- La base de conocimiento se aprende de tus `/compa sync`: cuanto más juegues y sincronices, más sabe la app.
