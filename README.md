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

- **Opcionales: Questie y ForeverDungeonJournal.** El addon no los necesita. Sin ellos usa los datos del propio juego (misiones que se pueden coger en tu zona, siguiente paso de cada misión, recompensas de tu registro, diario de mazmorras de Blizzard si existe) y lo que aprende mientras juegas (PNJ, entrenadores, misiones vistas). Si los instalas, añaden más detalle: `/compa estado` indica qué fuentes están activas (juego, Questie, ForeverDungeonJournal, aprendido). Las respuestas de la app dicen de dónde viene un dato solo cuando importa.

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
- **Menú contextual**: clic derecho en la cabecera para **Resumen de sesión** (un resumen corto de lo que llevas jugado, sin llamar a la IA), **Analizar sesión con IA** (el mismo resumen y 2 consejos de la IA), **Limpiar historial** o **Salir**. **Salir** cierra la aplicación de verdad.
- **Tamaño**: arrastra los bordes o la esquina para cambiarlo; la posición y el tamaño se recuerdan en `window.json`.
- Escribe tu pregunta y pulsa **Enter** (**Shift+Enter** para un salto de línea). **Escape** oculta el panel.
- **Incluir captura**: envía una captura de la ventana de WoW para preguntas sobre lo que ves en pantalla.
- **Enviar** hace lo mismo que Enter. Mientras la IA responde, la barra inferior muestra «Pensando…».
- La barra inferior muestra el estado: WoW conectado (con nivel y zona), franja de píxeles no encontrada, o la antigüedad de los datos de SavedVariables.
- Los consejos proactivos (al subir de nivel, completar objetivos, entregar una misión, cambiar de zona, cambiar una profesión o aceptar una misión) aparecen en la conversación como tarjetas destacadas con «💡 Consejo». Como mucho uno por minuto y nunca en combate. Si el panel está oculto, aparece sin robar el foco del juego.
- Los enlaces se abren en tu navegador (solo http y https).
- El DPI se respeta: el panel se escala en pantallas con 125 %, 150 % y otros valores.
- **👍 / 👎**: bajo la última respuesta puedes valorarla. Con 👎 se abre una línea opcional para decir qué falló. Las valoraciones se guardan en `companero.db` con la pregunta, la respuesta, el proveedor y la versión de la skill (ver [Valoraciones](#valoraciones-y-exportación)).
- **En combate** el panel se oculta solo (con `hideInCombat`, activado por defecto) y vuelve dos segundos después de acabar el combate. Los consejos que lleguen mientras tanto esperan en la conversación. Si abres el panel con el atajo durante un combate, se queda abierto.
- **Bandeja**: el icono junto al reloj abre o oculta el panel con clic izquierdo; con clic derecho ofrece Mostrar/ocultar, Ajustes, Resumen de sesión y Salir. Cerrar el panel no quita el icono.
- **Ajustes…** (menú contextual o bandeja): atajo, opacidad (60–100 %), tamaño de letra (9–14 pt), orden de proveedores, ocultar en combate, consejos proactivos, iniciar con WoW, carpeta de WoW y el estado de la clave de NVIDIA. Se guardan en `config.json`. Lo que se puede, se aplica al momento (atajo, opacidad, tamaño de letra, combate, inicio con WoW); lo demás se aplica al reiniciar y la propia pantalla lo dice.
- **Asistente de configuración…** (menú contextual o bandeja): comprueba la carpeta de WoW Forever, el addon, la sesión de Antigravity y de Codex, y la clave de NVIDIA, con ✅ o ❌ por cada punto. Puede instalar el addon desde la carpeta `addon\CompaneroWoW` junto al programa, pero solo cuando pulsas «Instalar addon». Solo lee el sistema y ejecuta los comandos de estado (`agy models`, `codex login status`).
- El asistente se abre solo la primera vez (cuando se crea `config.json`) y con `--setup`.

## Primer uso

1. Abre World of Warcraft en modo **sin bordes** (Opciones de vídeo → Modo de ventana): la app lee una franja de píxeles en la esquina superior izquierda, y debe verse.
2. Instala el addon y escribe `/compa sync` en el juego para cargar tus datos.
3. Sigue el asistente de configuración: marca lo que falte y pulsa **Comprobar de nuevo**.

## Inicio con WoW

En **Ajustes** marca **Iniciar con WoW**: la app se registra en la clave `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` y arranca al iniciar sesión en Windows, minimizada en la bandeja (`--tray`). Mientras WoW no esté abierto no hace nada, y la lectura del juego reintenta cada 5 segundos. La descripción de la bandeja muestra el estado de WoW. No se inyecta nada en el juego ni se instalan hooks. Desmarcar la casilla quita la entrada.

## Valoraciones y exportación

Cada 👍 o 👎 queda en la tabla `feedback` de `companero.db`. Para exportarlas a un archivo:

```
CompaneroWoW.exe --export-feedback valoraciones.json
```

El archivo tiene los totales y, por cada valoración, la fecha, la puntuación (+1 o -1), el motivo, la pregunta, la respuesta, el proveedor y la versión de la skill. La lista de herramientas usadas no se guarda todavía. Sirve también mientras la app está abierta.

## Pantalla de integración para desarrolladores

Otros componentes pueden añadir una opción al menú contextual de la cabecera con `ui.RegisterMenuAction(etiqueta, función)`. La función se ejecuta en el hilo de la interfaz: si tarda, debe lanzar su propia goroutine.

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
| `hideInCombat` | `true` (por defecto) oculta el panel mientras el personaje está en combate. |
| `startWithWoW` | `true` registra la app para arrancar al iniciar sesión, minimizada en la bandeja. Por defecto `false`. |
| `opacity` | Opacidad del panel en porcentaje, de 60 a 100 (por defecto 90). |
| `fontSize` | Tamaño del texto del panel en puntos, de 9 a 14 (por defecto 10). |
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

- `/compa sync`: fuerza el reenvío completo por el canal en vivo y guarda en SavedVariables los datos detallados (misiones, personaje, profesiones, etc.), con recarga de la interfaz. Con el canal en vivo ya no hace falta cada día: úsalo la primera vez, o si algo parece desactualizado en la app (por ejemplo, tras un cambio que la app no refleja). Lo que se ve en vivo lo sirve el canal; el archivo SavedVariables solo se escribe al recargar o al salir.
- `/compa strip on|off`: activa o desactiva la franja de píxeles (el canal en vivo).
- `/compa estado`: muestra el estado actual del addon, incluido el del canal en vivo.

## Canal en vivo (franja superior)

El addon dibuja en la franja superior de la ventana de WoW (todo el ancho, 4 px por bloque) los datos de tu personaje: dinero, experiencia, misiones con objetivos, bolsas, equipo, profesiones, misiones completadas, combates, barras de acción, conocimiento y estado. La app los lee de la pantalla, sin pulsar nada y sin leer memoria del juego. Va en vivo: el envío refresca cada 0,2 s, y cada mensaje se reenvía cada minuto para que una app que arranque tarde se ponga al día.

- En la barra de estado, `Canal en vivo: OK (x KB/s)` indica que llegan datos. `--diag` muestra la antigüedad de cada tipo de mensaje.
- Solo se pide `/compa sync` la primera vez o si algo parece desactualizado. Los datos que no viajan por el canal (por ejemplo, un personaje que nunca has sincronizado) siguen dependiendo de SavedVariables.
- La franja ocupa la parte superior de la ventana de WoW: la ventana de la app no la tapa.

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
| `analizar_rotacion` | Clase, nivel, especialización (la guardada o la deducida de los talentos), talentos y los hechizos conocidos con rango y etiquetas aproximadas. Trae la búsqueda web sugerida en inglés para la rotación. |
| `revisar_combates` | Últimos combates grabados (hasta 10; `ultimo`, `muertes` y `n` filtran): duración, dps, hechizos con su porcentaje, daño recibido, muertes con el asesino y los últimos golpes, y pistas calculadas sobre la ventana. Requiere `/compa sync` tras un combate. |
| `consejo_barras` | Barras de acción: ranura, tecla, hechizos repetidos, teclas lejanas, hechizos importantes sin colocar y una propuesta de ranuras libres. |
| `generar_comando_barras` | Convierte una distribución (ranura y hechizo) en comandos `/compa barras aplicar` válidos (hasta 40 pares, 255 caracteres cada uno). Solo se copian; la app nunca los envía al juego. |
| `que_entrenar` | Hechizos de clase que puede aprender ahora (y del siguiente nivel) con su coste, si le alcanza el oro y el entrenador de su clase cuando se conoce. Requiere WhatsTraining. |
| `ruta_profesion` | Cómo subir una profesión (SkillUpForever): recetas del siguiente rango, materiales, lista de la compra y entrenador. Acepta `profesion` o `skill_line_id`. Requiere SkillUpForever. |
| `analizar_equipo` | Mejoras por ranura con lo accesible ya («ahora») o pronto (hasta 3 niveles más): objeto actual, diferencia de nivel de objeto y de stats, misión o mazmorra de origen y su `/compa marca`. Acepta `ventana` (`ahora`, `pronto`, `ambas`) y `slot`. Requiere el equipo, las misiones accesibles y los candidatos que exporta el addon (`/compa sync`). |
| `misiones_disponibles` | Misiones que el personaje puede hacer ya o pronto, agrupadas por zona y ordenadas por distancia. Acepta `ventana` y `zona`. Marca las recompensas que son mejoras de equipo. |
| `info_mazmorra` | Una mazmorra de su rango de nivel (sin `nombre`, sugerencias): entrada con su marca, misiones relacionadas y botín que mejora a cada jefe. Requiere ForeverDungeonJournal. |
| `buscar_objeto` | De dónde sale un objeto (`nombre` o `item_id`): misión o jefe, stats, si su clase lo puede usar y si mejora su equipo. |
| `resumen_sesion` | Resumen de la sesión en curso o la última: duración, XP, oro, misiones, zonas y muertes. |

Las herramientas de situación `contexto_jugador`, `guia_mision` y `plan_profesiones` también usan estos datos: el contexto muestra solo recuentos (mejoras de equipo y misiones accesibles ahora) y el primer elemento de cada bloque.

## Otros addons y sesiones

El addon lee dos addons opcionales a través de sus APIs públicas y los guarda en SavedVariables junto a sus datos (fuera de combate, como mucho cada 10 s, al entrar y con `/compa sync`):

- **WhatsTraining**: hechizos de clase disponibles y del siguiente nivel, con su coste en cobre.
- **SkillUpForever**: la ruta de cada profesión (recetas, materiales y lista de la compra).

Si uno de ellos no está instalado o falla, la herramienta correspondiente dice qué falta en lugar de dar error. El addon también cuenta los errores que ha capturado sin mostrarlos (`health`), y `estado_conexion` los muestra para diagnosticar.

Una **sesión** empieza con el primer dato del puente tras 10 minutos sin datos, y termina tras otros 10 minutos sin datos o al cerrar la app. Se guarda en la base de datos local (tabla `sessions`): hora de inicio y fin, nivel y XP ganados, cambio de oro, misiones completadas y entregadas, zonas visitadas y muertes. Al arrancar, las sesiones que quedaron abiertas por un cierre brusco se cierran en su último dato.

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
