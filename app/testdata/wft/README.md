# Fixtures de wowforevertalents.com

Copias de páginas de **wowforevertalents.com**, descargadas el **10 de noviembre de 2026**
con el cliente de la app (User-Agent `CompaneroWoW/0.1`, una petición por segundo, respetando
`robots.txt`). Sirven solo para las pruebas de los analizadores (`internal/websearch`), que no
hacen ninguna petición de red.

| Archivo | Página |
| --- | --- |
| `front-page.html` | `https://wowforevertalents.com/` (fecha de parche «9 Nov») |
| `robots.txt` | `https://wowforevertalents.com/robots.txt` |
| `abilities-priest.html` | `/abilities/priest/` (hechizos de entrenador y competencias) |
| `abilities-warrior.html` | `/abilities/warrior/` |
| `build-priest-discipline-19-18-0.html` | `/priest/builds/discipline-19-18-0-6a17f2/` (build de la comunidad) |
| `talent-priest-blackout.html` | `/priest/talents/blackout/` |
| `dungeons.html` | `/dungeons/` (lista de mazmorras, bandas y campos de batalla) |
| `dungeon-ahn-qiraj-temple.html` | `/dungeons/ahn-qiraj-temple/` |
| `profession-blacksmithing.html` | `/professions/blacksmithing/` (recetas) |

El contenido pertenece a sus autores y se usa solo como dato de prueba. Para actualizarlo,
volver a descargar con `WFT_LIVE=1` (ver `wft_live_test.go`), nunca a mano.
