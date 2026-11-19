# Formato de la bandeja (outbox) del canal: msgType 7, 9, 12 y 16
Documento de contrato entre el addon (`CompaneroWoW/`) y la app (ingesta a SQLite). El addon es la
fuente de verdad de la forma; si cambia, cambia este documento y `CompaneroWoW/Storage.lua` a la vez.
Referencias en el addon:
- `CompaneroWoW/Storage.lua`: bandeja, tabla de claves (`Storage.KEYS`), redondeo (`Storage.ROUND`),
  claves volatiles (`Storage.DROP`), escape de claves, topes.
- `CompaneroWoW/Channel.lua` (cabecera): trama v3, trozos, `msgId`, `msgType`.
- `tools/lua/test_storage.lua`: ida y vuelta de la codificacion compacta y topes.
- `tools/lua/test_world.lua`, `tools/lua/test_session.lua`, `tools/lua/test_channel.lua`: envio.
## 1. Transporte
Cada mensaje del canal es un JSON en UTF-8 (reensamblado de trozos por `msgId`, ver
`Channel.lua`). El `msgType` de la trama indica el tipo. El cuerpo de un mensaje de la bandeja es un
**objeto JSON** con exactamente tres claves: `oseq`, `p` y `v`. Las claves salen ordenadas y sin espacios
(`{"oseq":3,"p":[...],"v":{...}}`).
Los mensajes de estado actual (tipos 1, 2, 3, 4, 5, 6, 8, 10, 11, 13, 14 y el bloque de hoja del tipo 12)
**no** son de la bandeja: no llevan `oseq`. Para distinguir: un mensaje es de la bandeja si y solo si es
un objeto con la clave `oseq`.
## 2. Envelope
| Campo | Tipo | Significado |
|-------|------|-------------|
| `oseq` | entero >= 1 | Numero de orden del item. Unico y creciente dentro de la misma instalacion del addon (la bandeja es de la cuenta: `CompaneroWoWDB`). Puede haber huecos (items que se han descartado). |
| `p` | array | Ruta del registro. Los elementos son cadenas o enteros y **no** se codifican (ver tabla por tipo). |
| `v` | objeto o array | El registro en **forma compacta** (ver seccion 4). Es el registro completo, no un delta. |
El `msgType` del mensaje (cabecera de la trama) dice que tipo de registro es. El campo `at` del item
(hora local de aprendizaje) **no** viaja en el canal.
## 3. Tipos de la bandeja y rutas
| msgType | Registro | `p` | `v` |
|---------|----------|-----|-----|
| 7 | Combate | `["combats", id]` | Combate (seccion 5.1) |
| 9 | Conocimiento | `["quests", questID]` o `["npcs", npcID]` | Mision o PNJ (5.2) |
| 12 | Hoja, cambio | `["sheet", dia]` | Hoja (5.3). `dia` = segundos epoch / 86400 (entero) |
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
### 5.3 Hoja de personaje, cambio (msgType 12)
`p = ["sheet", dia]`. `v` es la hoja (`Sheet.lua`, `SH.Collect`) sin `exportedAt` y sin `tooltip` de los
hechizos. Claves de primer nivel habituales: `level` (`lv`), `class`, `race`, `stance`, `base`, `total`,
`health`, `healthMax`, `power`, `powerMax`, `powerType`, `armor`, `armorEffective`, `crit`, `haste`, `hit`,
`defense`, `melee`, `ranged`, `regen`, `resist`, `spell`, `talents`, y, si hay datos, `weaponSkills`,
`spells`, `buffs`. Los hechizos llevan `spellID`, `n` (nombre), `rank`, `known` y los campos que anade `spellNumbers` (`Sheet.lua`). Las claves anidadas viajan con su nombre largo.
```json
{"oseq":11,"p":["sheet",20000],"v":{"class":"WARRIOR","health":4200,"healthMax":4200,"lv":60,"melee":{"ap":1200,"crit":12.5},"race":"Human","spells":[{"n":"Golpe furtivo","rank":"","spellID":1752}]}}
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
1. **Idempotencia por `oseq`**: el mismo `oseq` puede llegar mas de una vez (un item sale en hasta 3
   ventanas de envio). Un `oseq` ya guardado se ignora.
2. **Registro completo, ultimo gana**: cada `v` es el registro entero de su ruta. Para una ruta
   `(msgType, p)` se guarda el de `oseq` mayor. Si llega un `oseq` menor que el guardado, se ignora.
   Para el tipo 12, cada dia puede tener varios items: el de `oseq` mayor es el estado del dia.
3. **Sin borrados**: no existe mensaje de borrado. Un registro que deja de llegar conserva su ultima copia.
4. **Huecos son normales**: el addon descarta items (tope de tamano o de cantidad, o tras 3 ventanas).
   Un `oseq` que nunca llega no es un error.
5. **Orden de llegada**: no se garantiza. Se decide siempre por `oseq`, no por el orden de recepcion.
6. **Mensajes de estado actual** (sin `oseq`) se tratan aparte: reemplazan el bloque anterior del
   personaje (ver `Channel.lua`, `C.TYPES`).
## 7. Topes y retencion (lado addon)
- Bandeja: tope principal de **60 KB serializados** (`Storage.MAX_SV_BYTES`, tamano del texto que
  ocuparia en SavedVariables, no del JSON) y tope secundario de **2000 items** (`Storage.MAX_ITEMS`).
  Al pasarse, se descarta lo mas antiguo.
- Un item sale en una ventana de envio cuando se envia completo. Cuando ha salido en **3 ventanas**
  distintas, se quita de la bandeja. Una ventana es una activacion de envio que empieza desde reposo.
- `/compa limpiar si` vacia la bandeja; el `oseq` siguiente sigue creciendo.
- La migracion desde el esquema 1 crea items nuevos (con `oseq` nuevos) a partir del conocimiento,
  el mundo y los combates que habia en SavedVariables.
