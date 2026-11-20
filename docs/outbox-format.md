# Formato de la bandeja (outbox) y del estado actual del canal: msgType 7, 9 y 16 (bandeja) y el
# estado `characters[clave].state` (esquema 2.1)
Documento de contrato entre el addon (`CompaneroWoW/`) y la app (ingesta a SQLite). El addon es la
fuente de verdad de la forma; si cambia, cambia este documento y `CompaneroWoW/Storage.lua` a la vez.
Referencias en el addon:
- `CompaneroWoW/Storage.lua`: bandeja, tabla de claves (`Storage.KEYS`), redondeo (`Storage.ROUND`),
  claves volatiles (`Storage.DROP`), escape de claves, topes, estado actual (`Storage.SaveState`).
- `CompaneroWoW/Channel.lua` (cabecera): trama v3, trozos, `msgId`, `msgType`.
- `tools/lua/test_storage.lua`: ida y vuelta de la codificacion compacta y topes.
- `tools/lua/test_world.lua`, `tools/lua/test_session.lua`, `tools/lua/test_channel.lua`: envio.
## 1. Transporte
Cada mensaje del canal es un JSON en UTF-8 (reensamblado de trozos por `msgId`, ver
`Channel.lua`). El `msgType` de la trama indica el tipo. El cuerpo de un mensaje de la bandeja es un
**objeto JSON** con exactamente tres claves: `oseq`, `p` y `v`. Las claves salen ordenadas y sin espacios
(`{"oseq":3,"p":[...],"v":{...}}`).
Los mensajes de estado actual (tipos 1, 2, 3, 4, 5, 6, 8, 10, 11, 12, 13, 14) **no** son de la bandeja:
no llevan `oseq`. Para distinguir: un mensaje es de la bandeja si y solo si es un objeto con la clave
`oseq`. La hoja (12) viaja en el canal como estado actual y tambien se guarda en el estado (seccion 8).
## 2. Envelope
| Campo | Tipo | Significado |
|-------|------|-------------|
| `oseq` | entero >= 1 | Numero de orden del item. Unico y creciente dentro de la misma instalacion del addon (la bandeja es de la cuenta: `CompaneroWoWDB`). Puede haber huecos (items que se han descartado por caducidad o topes). |
| `p` | array | Ruta del registro. Los elementos son cadenas o enteros y **no** se codifican (ver tabla por tipo). |
| `v` | objeto o array | El registro en **forma compacta** (ver seccion 4). Es el registro completo, no un delta. |
El `msgType` del mensaje (cabecera de la trama) dice que tipo de registro es. El campo `at` del item
(hora local de aprendizaje) **no** viaja en el canal.
## 3. Tipos de la bandeja y rutas
| msgType | Registro | `p` | `v` |
|---------|----------|-----|-----|
| 7 | Combate | `["combats", id]` | Combate (seccion 5.1) |
| 9 | Conocimiento | `["quests", questID]` o `["npcs", npcID]` | Mision o PNJ (5.2) |
| 16 | Mundo | ver abajo | Segun ruta (5.4) |
Rutas del tipo 16:
- `["flight", faccion, "nodes", nodeID]`: nodo de vuelo. `faccion` = `"Alliance"`, `"Horde"` o `"Neutral"`.
- `["flight", faccion, "costs", "<origen>><destino>"]`: coste medido de un trayecto (IDs de nodo).
- `["flight", "known", "<Nombre>-<Reino>"]`: `v` es un array de `nodeID` conocidos por el personaje.
- `["vendors", npcID]`: vendedor.
- `["trainers", npcID]`: instructor.
- `["questChains", questID]`: cadena de la mision.
- `["consumables", itemID]`: consumible.
## 4. Codificacion compacta
Aplica a todo `v` (en todos los tipos). Se produce con `Storage.Compact` y se invierte con
`Storage.Expand`.
### 4.1 Tabla de claves (largo -> corto)
| Largo | Corto | Largo | Corto | Largo | Corto |
|-------|-------|-------|-------|-------|-------|
| name | n | giverNpcID | gn | copper | cu |
| mapID | m | giverName | gna | hops | ho |
| x | x | giverMapID | gm | seconds | se |
| y | y | giverX | gx | status | st |
| reaction | r | giverY | gy | slot | sl |
| isQuestGiver | qg | turnInNpcID | tn | nodeID | nd |
| isVendor | vd | turnInName | tna | known | kn |
| isTrainer | tr | turnInMapID | tm | offeredAt | oa |
| availableCount | ac | turnInX | tx | offeredSource | os |
| services | sv | turnInY | ty | npcID | pn |
| firstSeen | fs | title | ti | questID | qd |
| level | lv | objectivesText | ot | descriptionText | dt |
| rewards | rw | money | mo | xp | xp |
| items | it | itemID | i | link | l |
| count | c | choice | ch | | |
Las claves que no estan en la tabla viajan con su nombre largo (p. ej. `class`, `race`, `zone`,
`duration`, `targets`, `faction`, `kind`, `profession`, `pre`, `next`, `restores`, `requiredLevel`,
`skill`, `startedAt`, `endedAt`, `died`, `inInstance`, `id`, `source`, `spellName`, `amount`...).
### 4.2 Reglas
1. **Redondeo**: los valores numericos de las claves `x`, `y`, `giverX`, `giverY`, `turnInX`, `turnInY`
   (en cualquier nivel de anidamiento) se redondean a 3 decimales.
2. **Volatiles**: `lastSeen` y `lastOffered` nunca se envian.
3. **Escape**: una clave sin codigo corto que coincide con uno (`n`, `c`, `i`, `m`, `r`, `x`, ...) o que
   empieza por `~` se escribe con un `~` delante. Ejemplo: una clave `n` sin traducir viaja como `~n`.
   En la practica no aparece en los datos actuales, pero el decodificador debe tratarlo.
4. Los arrays se conservan (su orden tambien). Los objetos se recorren recursivamente.
### 4.3 Decodificacion (para la app)
```
decodeKey(k):
    if k empieza por "~":          return k sin el primer "~"
    if k esta en CORTO_A_LARGO:    return CORTO_A_LARGO[k]
    return k
decodeValue(v):                     # recursivo sobre objetos y arrays
    objeto: { decodeKey(k): decodeValue(val) }
    array:  [ decodeValue(x) ... ]
    otro:   v
```
`p` **no** se decodifica: sus elementos son literales (`"quests"`, `"flight"`, `"Alliance"`...).
## 5. Forma de los registros (ejemplos reales)
Los ejemplos salen del propio codigo (`Storage.Learn` + `Json.Encode` del payload) con datos de prueba.
### 5.1 Combate (msgType 7)
Campos de nivel superior del registro largo (`Combat.lua`, `C.Build`): `id`, `startedAt`, `endedAt`
(segundos epoch), `duration` (s), `died`, `zone`, `mapID`->`m`, `inInstance`, `targets`,
`damageDone` (`total`, `spells`), `healingDone` (`total`, `spells`), `damageTaken` (`total`, `sources`),
`death` (`killer`, `lastHits`), `resourcesAtEnd` (`healthPct`, `powerPct`).
Filas de hechizos: `{spellID, amount, n}` (`n` = nombre). Filas de fuentes de dano: `{name, spellID, amount}`.
`targets`: `{name, ...}` segun `readTarget` (`Combat.lua`). `lastHits`: `{source, spellName, amount}`.
```json
{"oseq":10,"p":["combats",3],"v":{"damageDone":{"spells":[{"amount":2100,"n":"Golpe furtivo","spellID":1752}],"total":3000},"died":false,"duration":42.1,"endedAt":1700000042,"id":3,"inInstance":false,"m":1429,"resourcesAtEnd":{"healthPct":80,"powerPct":60},"startedAt":1700000000,"targets":[{"guid":"Creature-0-0-0-0-1234-0","n":"Lobo"}],"zone":"Elwynn"}}
```
### 5.2 Conocimiento (msgType 9)
Mision (`p = ["quests", questID]`). Claves largas: `questID`, `title`, `level`, `objectivesText`,
`descriptionText`, `rewards{money, xp, items[{itemID, link, count, choice}]}`, `giverNpcID`, `giverName`,
`giverMapID`, `giverX`, `giverY`, `turnInNpcID`, `turnInName`, `turnInMapID`, `turnInX`, `turnInY`,
`offeredAt{mapID, x, y}`, `offeredSource`, `firstSeen`.
```json
{"oseq":1,"p":["quests",9001],"v":{"dt":"Los lobos atacan la granja.","fs":1700000000,"gm":1429,"gn":1234,"gna":"Granjero Tomas","gx":0.251,"gy":0.663,"lv":5,"oa":{"m":1429,"x":0.25,"y":0.66},"os":"juego","ot":"Mata 8 lobos.","qd":9001,"rw":{"it":[{"c":1,"ch":true,"i":2901,"l":"|Hitem:2901|h[Bolsa]|h"}],"mo":1200,"xp":450},"ti":"Peligro en la granja","tm":1429,"tn":1240,"tna":"Capitan Vale","tx":0.3,"ty":0.7}}
```
PNJ (`p = ["npcs", npcID]`). Claves largas: `npcID`, `name`, `mapID`, `x`, `y`, `reaction`,
`isQuestGiver`, `isVendor`, `isTrainer`, `availableCount`, `services` (array de cadenas), `firstSeen`.
```json
{"oseq":2,"p":["npcs",1234],"v":{"ac":2,"fs":1700000000,"m":1429,"n":"Granjero Tomas","pn":1234,"qg":true,"r":5,"sv":["Peligro en la granja"],"x":0.251,"y":0.663}}
```
### 5.3 Hoja de personaje (ya no es msgType 12 de la bandeja)
La hoja no se guarda en la bandeja desde el esquema 2.1: es el bloque `sheet` del estado (seccion 8).
Su forma: `Sheet.lua`, `SH.Collect`, sin `exportedAt` y sin `tooltip` de los hechizos (los numeros del
hechizo, `castTime`, `range`, `cost`, `cooldown`, si se pudieron leer, se quedan). Claves de primer nivel
habituales: `level`, `class`, `race`, `stance`, `base`, `total`, `health`, `healthMax`, `power`,
`powerMax`, `powerType`, `armor`, `armorEffective`, `melee`, `ranged`, `defense`, `spell`, `regen`,
`resist`, `talents` (arboles con `name`, `spellID`, `rank`, `maxRank`), y, si hay datos, `weaponSkills`,
`spells` (`spellID`, `name` en el idioma del cliente, `rank`, `known`, y los numeros) y `buffs`.
```json
{"sheet":{"class":"WARRIOR","level":60,"race":"Human","spells":[{"cooldown":6,"known":true,"name":"Golpe heroico","rank":"","spellID":78}],"talents":{"tabs":[{"name":"Armas","talents":[{"name":"Golpe mortal","spellID":12294,"rank":5,"maxRank":5}]}]}}}
```
### 5.4 Mundo (msgType 16)
Nodo de vuelo (`nodeID`->`nd`, `name`->`n`, `mapID`->`m`, `x`, `y`, `known`->`kn`):
```json
{"oseq":3,"p":["flight","Alliance","nodes",1001],"v":{"kn":true,"m":1429,"n":"Puerto de Ventobravo","nd":1001,"x":0.441,"y":0.512}}
```
Coste de trayecto (`copper`->`cu`, `hops`->`ho`, `seconds`->`se`):
```json
{"oseq":4,"p":["flight","Alliance","costs","1001>1002"],"v":{"cu":1250,"ho":1,"se":75}}
```
Nodos conocidos del personaje (array de `nodeID`, sin claves):
```json
{"oseq":5,"p":["flight","known","Tester-Ravencrest"],"v":[1001,1002,1003]}
```
Vendedor (`npcID`->`pn`, `items[{itemID, name, price, stack, limited}]`->`it`):
```json
{"oseq":6,"p":["vendors",1000],"v":{"faction":"Alliance","it":[{"i":2001,"limited":false,"n":"Pan","price":1,"stack":1}],"m":1429,"n":"Vendedor de Pan","pn":1000,"x":0.5,"y":0.5}}
```
Instructor (`kind`, `profession`, `services[{name, rank, cost, level, available, skill?}]`->`sv`):
```json
{"oseq":7,"p":["trainers",2000],"v":{"kind":"profession","m":1429,"n":"Instructor de Herboristeria","pn":2000,"profession":"Herboristeria","sv":[{"available":"available","cost":10000,"lv":5,"n":"Herboristeria","rank":"1"}],"x":0.3,"y":0.3}}
```
Cadena de mision (`pre` puede faltar; `next` puede faltar):
```json
{"oseq":8,"p":["questChains",9001],"v":{"next":9002,"pre":[9000]}}
```
Consumible (`itemID`->`i`, `restores{health, mana, seconds->se}`, `requiredLevel`, `skill?`):
```json
{"oseq":9,"p":["consumables",2001],"v":{"i":2001,"kind":"food","n":"Pan","restores":{"health":0,"mana":0,"se":0}}}
```
## 6. Reglas de ingesta
1. **Idempotencia por `oseq`**: el mismo `oseq` puede llegar mas de una vez (un item sale en cada
   ventana de envio mientras no caduque). Un `oseq` ya guardado se ignora.
2. **Registro completo, ultimo gana**: cada `v` es el registro entero de su ruta. Para una ruta
   `(msgType, p)` se guarda el de `oseq` mayor. Si llega un `oseq` menor que el guardado, se ignora.
3. **Sin borrados**: no existe mensaje de borrado. Un registro que deja de llegar conserva su ultima copia.
4. **Huecos son normales**: el addon descarta items (tope de tamano o de cantidad, o por caducidad a
   los 7 dias). Un `oseq` que nunca llega no es un error.
5. **Orden de llegada**: no se garantiza. Se decide siempre por `oseq`, no por el orden de recepcion.
6. **Mensajes de estado actual** (sin `oseq`) se tratan aparte: reemplazan el bloque anterior del
   personaje (ver `Channel.lua`, `C.TYPES`). El estado guardado (seccion 8) sigue la misma regla: cada
   seccion se reemplaza entera.
## 7. Topes y retencion (lado addon)
- Bandeja: tope principal de **60 KB serializados** (`Storage.MAX_SV_BYTES`, tamano del texto que
  ocuparia en SavedVariables, no del JSON) y tope secundario de **2000 items** (`Storage.MAX_ITEMS`).
  Al pasarse, se descarta lo mas antiguo.
- **Caducidad**: un item se quita de la bandeja al cumplir **7 dias** desde su hora de aprendizaje
  (`at`, `Storage.OUTBOX_MAX_AGE`). Un item sin hora (`at` = 0) no caduca.
- **Sin borrado por envios**: salir en una ventana no quita el item (el addon no tiene confirmacion de
  que la app lo haya recibido). Cada ventana de envio reenvia la bandeja: primero los items que nunca han
  salido, despues los que menos veces han salido (`shares`, contado al salir entero).
- `/compa limpiar si` vacia la bandeja; el `oseq` siguiente sigue creciendo.
- La migracion desde el esquema 1 crea items nuevos (con `oseq` nuevos) a partir del conocimiento,
  el mundo y los combates que habia en SavedVariables.
- La migracion desde el esquema 2 (sin `schemaMinor`) saca los items de tipo 12 de la bandeja: la hoja
  mas reciente pasa al estado del personaje activo (seccion 8).

## 8. Estado actual: `characters[clave].state` (esquema 2.1)
`CompaneroWoWDB.schema = 2` y `CompaneroWoWDB.schemaMinor = 1` (la app lo lee en la ingesta de
SavedVariables; sin `state` el fichero es 2.0). El estado es la **ultima copia** de cada bloque del
personaje: se sobrescribe, no es un historial y no va a la bandeja.

| Seccion | Origen en el addon | Contenido |
|---------|--------------------|-----------|
| `sheet` | `Sheet.lua` (`SH.Collect`) | Hoja sin `exportedAt` y sin `tooltip` de los hechizos |
| `equipment` | `Collectors.lua` (`CollectEquipment`) | Equipo por ranura (1..19), con `link`, `itemID`, `itemLevel` |
| `gear` | `Gear.lua` (`CollectGear`) | Gear del personaje (fuentes y calidades) |
| `proficiencies` | `Gear.lua` | Competencias (armas, armaduras) |
| `professions` | `Collectors.lua` (`CollectProfessions`) | Profesiones con `rank` y `maxRank` |
| `training` | `Integrations.lua` (WhatsTraining) | Entrenamiento pendiente |
| `profPlan` | `Integrations.lua` (SkillUpForever) | Plan de profesiones y listas de compra |
| `quests` | `Collectors.lua` (`CollectQuests`) | Registro de misiones: `questID`, `title`, `level`, `zoneOrSort`, `isComplete`, `isFailed`, `isTracked`, `objectives` |
| `location` | `Collectors.lua` (`CollectLocation`) | Ubicacion: `mapID`, `zone`, `subZone`, `x`, `y`, `instanceName`, `instanceType` |
| `at` | Storage.lua | Hora (Unix) de la ultima escritura de un bloque |

Las claves van **en formato largo** (como en el canal), sin codificacion compacta: es un bloque de
lectura, no de la bandeja.

```lua
characters["Tester-Ravencrest"].state = {
  at = 1700000000,
  sheet = { class = "WARRIOR", level = 60, race = "Human", total = { ... }, talents = { ... },
            spells = { { spellID = 78, name = "Golpe heroico", rank = "", known = true, cooldown = 6 } } },
  equipment = { [1] = { slotID = 1, itemID = 2, link = "|Hitem:2|h[...]|h|r", itemLevel = 40, quality = 4 } },
  location = { mapID = 1429, zone = "Elwynn", x = 0.41, y = 0.52 },
  -- ... los demas bloques de la tabla, si hay datos
}
```

Reglas:
- **Escritura**: `Storage.SaveState` tras cada cambio de bloque (snapshot, hoja, entrenamiento, plan,
  equipo). Solo escribe si algun bloque cambio; `at` se actualiza entonces.
- **Lectura vacia no borra**: un bloque sin dato nuevo (nil o vacio) conserva la copia anterior.
- **Presupuesto por personaje**: **60 KB** serializados (`Storage.STATE_MAX_BYTES`). Si no cabe, se quitan
  bloques en este orden: `training`, `profPlan`, `gear`, `proficiencies`, `quests`, `professions`,
  `location`, `equipment`; despues los hechizos de la hoja, desde el ultimo.
- **Fichero**: si el fichero entero pasa de **150 KB** (`Storage.FILE_TARGET_BYTES`), el estado de los
  demas personajes se recorta a su nucleo: `sheet` con `level`, `class`, `race`, `total` y `talents`.
- **Ingesta (app)**: cada seccion se guarda en `char_state` (la forma que leen los lectores). Un bloque
  mas nuevo (por `at`) reemplaza al anterior; a igual hora gana el de mayor `oseq`, como en el canal.
