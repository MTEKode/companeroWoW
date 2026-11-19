# Vistas de consulta (consultar_db)

La base de datos de la app guarda lo que el addon envía: personajes, misiones, PNJ, vendedores,
instructores, vuelos, objetos, combates, sesiones, equipo, macros, barras y mazmorras. La herramienta
`consultar_db` lee **solo estas vistas**, en modo de solo lectura.

## Reglas

- Una sola sentencia `SELECT` o `WITH ... SELECT`. Nada de INSERT, UPDATE, DELETE, PRAGMA, ATTACH ni
  `sqlite_master`.
- Las tablas de base no se nombran: usa las vistas. Las tablas internas (chat, caché, comentarios) no son
  accesibles.
- Sin LIMIT, la consulta devuelve 100 filas. Con `limite` se pide hasta 500. Si hay más, la respuesta
  dice `truncado: true`.
- La consulta tiene 2 segundos. Si tarda más, se cancela: acótala con filtros.
- Las fechas de las vistas son texto `AAAA-MM-DD HH:MM:SS` en hora local. El dinero está en **cobre**
  (1 oro = 10000 cobre, 1 plata = 100 cobre).
- Las posiciones (`x`, `y`) son de 0 a 1 dentro de la zona. `zona_id` es el id de mapa del juego.
- `personaje` es la clave del personaje (`Nombre-Reino`). Vacía en las sesiones de la propia app.
- Los campos `*_json` son JSON en texto: usa `json_each` o `json_extract` si hace falta descomponerlos.

## v_personaje

Un registro por personaje con sus datos básicos.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje (`Nombre-Reino`). |
| nombre | Nombre del personaje. |
| reino | Reino. |
| raza | Raza. |
| clase | Clase. |
| nivel | Nivel actual. |
| xp | Experiencia en el nivel actual. |
| xp_max | Experiencia necesaria para subir del nivel actual. |
| dinero_cobre | Dinero en cobre. |
| actualizado | Último dato recibido del personaje. |

Ejemplo: `SELECT personaje, nivel, dinero_cobre FROM v_personaje`

## v_misiones

Las misiones del personaje: las que lleva en el registro y las que la ventana de misiones le ofrece.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| mision_id | Id de la misión. |
| titulo | Nombre de la misión. |
| nivel | Nivel de la misión. |
| zona | Zona donde se ofrece (si se conoce). |
| estado | `en_registro`, `lista_para_entregar`, `accesible` o `no_accesible`. |
| en_registro | 1 si la lleva en el registro ahora. |
| accesible | 1 si el personaje puede aceptarla ahora. |
| objetivos | Texto de los objetivos. |

Ejemplo: `SELECT titulo, zona FROM v_misiones WHERE personaje = 'Nombre-Reino' AND accesible = 1 ORDER BY nivel`

## v_misiones_conocidas

Todas las misiones que el addon ha aprendido jugando (no dependen del personaje).

| Columna | Descripción |
|---|---|
| mision_id | Id de la misión. |
| titulo | Nombre. |
| nivel | Nivel. |
| entrega_npc | PNJ que la da. |
| entrega_zona | Zona del PNJ que la da. |
| entrega_final_npc | PNJ al que se entrega. |
| entrega_final_zona | Zona del PNJ al que se entrega. |
| objetivos | Texto de los objetivos. |
| descripcion | Texto de la misión. |
| recompensa_cobre | Recompensa fija de dinero (cobre). |
| recompensa_xp | Recompensa de experiencia. |
| recompensas_objetos | JSON de objetos de recompensa: `objeto_id`, `cantidad`, `tipo` (`reward` fija o `choice` a elegir). |
| visto_ultima_vez | Última vez que el addon la vio. |

Ejemplo: `SELECT titulo, entrega_npc, entrega_zona FROM v_misiones_conocidas WHERE nivel BETWEEN 5 AND 10`

## v_npcs

PNJ conocidos, con su zona y sus roles.

| Columna | Descripción |
|---|---|
| npc_id | Id del PNJ. |
| nombre | Nombre. |
| zona | Zona. |
| zona_id | Id de mapa. |
| x, y | Posición en la zona (0 a 1). |
| roles | Roles separados por coma: `quest_giver`, `turnin`, `vendor`, `trainer`. |
| reaccion | Reacción hacia el personaje (si se conoce). |

Ejemplo: `SELECT nombre, zona FROM v_npcs WHERE roles LIKE '%trainer%'`

## v_vendedores_objetos

Qué vende cada vendedor: una fila por objeto.

| Columna | Descripción |
|---|---|
| vendedor_npc_id | Id del vendedor. |
| vendedor | Nombre del vendedor. |
| zona | Zona del vendedor. |
| faccion | Facción del vendedor. |
| objeto_id | Id del objeto. |
| objeto | Nombre del objeto. |
| precio_cobre | Precio en cobre. |
| cantidad_por_compra | Unidades por compra. |
| limitado | 1 si el stock es limitado. |
| requiere_reputacion | Reputación necesaria (si la hay). |

Ejemplo: `SELECT objeto, precio_cobre, vendedor FROM v_vendedores_objetos WHERE objeto LIKE '%tela%' ORDER BY precio_cobre`

## v_instructores_servicios

Qué enseña cada instructor, con coste y requisitos.

| Columna | Descripción |
|---|---|
| instructor_npc_id | Id del instructor. |
| instructor | Nombre. |
| zona | Zona. |
| tipo | Tipo de instructor (clase, profesión...). |
| profesion | Profesión (si aplica). |
| servicio | Nombre de lo que enseña. |
| rango | Rango de la enseñanza. |
| coste_cobre | Coste en cobre. |
| nivel_requerido | Nivel mínimo. |
| habilidad | Habilidad profesional requerida. |
| habilidad_requerida | Nivel de esa habilidad. |
| disponible | Estado de disponibilidad que indicó el addon. |

Ejemplo: `SELECT instructor, servicio, coste_cobre FROM v_instructores_servicios WHERE nivel_requerido <= 12`

## v_vuelos

Rutas de vuelo medidas entre nodos.

| Columna | Descripción |
|---|---|
| origen_id, origen | Nodo de origen. |
| origen_zona | Zona del origen. |
| destino_id, destino | Nodo de destino. |
| destino_zona | Zona del destino. |
| faccion | Facción del nodo de origen. |
| coste_cobre | Coste del vuelo en cobre. |
| saltos | Número de saltos. |
| segundos | Duración medida del vuelo. |
| origen_conocido_por | JSON con los personajes que conocen el nodo de origen. |

Ejemplo: `SELECT origen, destino, segundos FROM v_vuelos ORDER BY segundos DESC LIMIT 10`

## v_objetos

Objetos vistos: el registro, sus estadísticas y el daño de las armas.

| Columna | Descripción |
|---|---|
| objeto_id | Id del objeto. |
| nombre | Nombre. |
| enlace | Enlace del objeto. |
| nivel_objeto | Nivel del objeto. |
| nivel_requerido | Nivel necesario para usarlo. |
| calidad | Calidad (0 gris a 5 legendario). |
| ranura | Ranura de equipo (`INVTYPE_HEAD`...). |
| clase | Clase del objeto. |
| subclase | Subclase. |
| dano_min, dano_max, velocidad | Daño y velocidad (armas). |
| estadisticas | JSON con las estadísticas, por ejemplo `{"ITEM_MOD_STAMINA_SHORT": 9}`. |

Ejemplo: `SELECT nombre, nivel_objeto FROM v_objetos WHERE ranura = 'INVTYPE_HEAD' ORDER BY nivel_objeto DESC`

## v_consumibles

Comida, bebida y pociones, con lo que restauran.

| Columna | Descripción |
|---|---|
| objeto_id | Id del objeto. |
| nombre | Nombre. |
| tipo | Tipo (`food`, `drink`, `potion`...). |
| nivel_requerido | Nivel necesario. |
| vida | Vida que restaura. |
| mana | Maná que restaura. |
| segundos | Duración del efecto en segundos. |
| habilidad | Habilidad profesional que da (si aplica). |
| habilidad_requerida | Nivel de esa habilidad. |

Ejemplo: `SELECT nombre, vida FROM v_consumibles WHERE tipo = 'food' ORDER BY vida DESC`

## v_combates

Un registro por combate.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| combate_id | Número del combate del personaje. |
| inicio | Hora de inicio. |
| duracion_s | Duración en segundos. |
| zona | Zona. |
| dano_total | Daño causado. |
| dps | Daño por segundo. |
| curacion_total | Curación realizada. |
| dano_recibido | Daño recibido. |
| murio | 1 si el personaje murió. |
| vida_final_pct | Vida al final del combate (%), -1 si se desconoce. |
| objetivos_json | JSON con los nombres de los objetivos. |

Ejemplo: `SELECT inicio, zona, dps FROM v_combates ORDER BY inicio DESC LIMIT 20`

## v_combate_hechizos

Los hechizos de cada combate: daño causado (`dano`) o curación (`curacion`).

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| combate_id | Número del combate. |
| tipo | `dano` o `curacion`. |
| hechizo_id | Id del hechizo. |
| hechizo | Nombre del hechizo. |
| cantidad | Total causado o curado. |
| golpes | Número de golpes. |

Ejemplo: `SELECT hechizo, SUM(cantidad) AS total FROM v_combate_hechizos WHERE tipo = 'dano' GROUP BY hechizo ORDER BY total DESC LIMIT 10`

## v_muertes

Los combates que acabaron en muerte.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| combate_id | Número del combate. |
| momento | Hora de la muerte (aprox., inicio más duración). |
| zona | Zona. |
| asesino | Quién causó la muerte. |
| ultimos_golpes_json | JSON de los últimos golpes: `fuente`, `hechizo`, `cantidad`. |
| duracion_s | Duración del combate. |

Ejemplo: `SELECT zona, asesino, COUNT(*) AS muertes FROM v_muertes GROUP BY zona, asesino ORDER BY muertes DESC`

## v_sesiones

Sesiones de juego: las del addon (con `personaje`) y las que registra la app (sin `personaje`).

| Columna | Descripción |
|---|---|
| sesion_id | Id de la sesión en la app. |
| personaje | Clave del personaje (vacía si la registró la app). |
| inicio | Hora de inicio. |
| fin | Hora de fin (vacía si sigue abierta). |
| abierta | 1 si sigue abierta. |
| nivel_inicio, nivel_fin | Nivel al empezar y al acabar. |
| niveles_subidos | Niveles subidos. |
| xp_ganado_pct | Experiencia ganada, en porcentaje de un nivel. |
| dinero_inicio_cobre, dinero_fin_cobre | Dinero al empezar y al acabar (cobre). |
| misiones_completadas_json | JSON con los ids de misiones completadas. |
| misiones_entregadas_json | JSON con los ids de misiones entregadas. |
| zonas_json | JSON con los ids de mapa visitados. |
| muertes | Muertes en la sesión. |

Ejemplo: `SELECT inicio, fin, niveles_subidos FROM v_sesiones WHERE personaje = 'Nombre-Reino' ORDER BY inicio DESC LIMIT 5`

## v_equipo

Cada cambio de equipo por ranura. `actual = 1` es el objeto que lleva puesto ahora.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| ranura_id | Número de ranura (1 cabeza, 16 mano derecha...). |
| ranura | Tipo de ranura del objeto. |
| objeto_id | Id del objeto. |
| objeto | Nombre. |
| nivel_objeto | Nivel del objeto. |
| calidad | Calidad. |
| desde | Desde cuándo lleva el objeto. |
| actual | 1 si es el objeto puesto ahora. |

Ejemplo: `SELECT ranura, objeto, nivel_objeto FROM v_equipo WHERE personaje = 'Nombre-Reino' AND actual = 1`

## v_candidatos_equipo

Objetos equipables que el personaje puede conseguir (recompensas de misión, botín de mazmorra).

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| objeto_id | Id del objeto. |
| objeto | Nombre. |
| nivel_objeto | Nivel del objeto. |
| nivel_requerido | Nivel necesario. |
| ranura | Ranura de equipo. |
| calidad | Calidad. |
| fuente_tipo | De dónde sale (`quest`, `dungeon`...). |
| mision_id | Misión de la que sale (si aplica). |
| mazmorra | Clave de la mazmorra (si aplica). |
| jefe | Jefe que lo suelta (si aplica). |
| fuente | Fuente indicada por el addon. |
| a_elegir | 1 si es una recompensa a elegir. |
| estadisticas | JSON con las estadísticas. |

Ejemplo: `SELECT objeto, nivel_objeto, fuente_tipo FROM v_candidatos_equipo WHERE personaje = 'Nombre-Reino' ORDER BY nivel_objeto DESC`

## v_macros

Macros de cuenta y de personaje.

| Columna | Descripción |
|---|---|
| ambito | `account` (cuenta) o `character` (personaje). |
| personaje | Clave del personaje (vacía para las de cuenta). |
| indice | Posición de la macro. |
| nombre | Nombre de la macro. |
| cuerpo | Texto de la macro. |

Ejemplo: `SELECT nombre, cuerpo FROM v_macros WHERE ambito = 'character'`

## v_barras

Las ranuras de las barras de acción de cada personaje.

| Columna | Descripción |
|---|---|
| personaje | Clave del personaje. |
| ranura | Número de la ranura. |
| tipo | Tipo (`spell`, `item`, `macro`...). |
| nombre | Nombre de lo que hay en la ranura. |
| id | Id del hechizo u objeto. |
| tecla | Tecla asignada (si hay). |

Ejemplo: `SELECT ranura, nombre, tecla FROM v_barras WHERE personaje = 'Nombre-Reino' ORDER BY ranura`

## v_mazmorras

Mazmorras con su rango de niveles, sus jefes y su botín.

| Columna | Descripción |
|---|---|
| mazmorra_clave | Clave de la mazmorra. |
| nombre | Nombre. |
| zona | Zona de la entrada. |
| nivel_min, nivel_max | Rango de niveles. |
| fuente | Fuente de los datos. |
| misiones_json | JSON con los ids de misiones de la mazmorra. |
| jefes_json | JSON con los nombres de los jefes. |
| botin_json | JSON con el botín: `jefe` y `objeto_id` por cada objeto. |

Ejemplo: `SELECT nombre, nivel_min, nivel_max FROM v_mazmorras WHERE nivel_max >= 10 ORDER BY nivel_min`
