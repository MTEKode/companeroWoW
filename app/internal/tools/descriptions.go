package tools

// Spanish descriptions shown to the AI. Each says when the tool is the right choice.
const (
	descEstado = "Indica si el puente con WoW está conectado, cuándo se actualizaron los datos guardados (SavedVariables) y qué personaje está activo. Úsala para diagnosticar por qué faltan datos antes de decir que no sabes algo."

	descUbicacion = "Devuelve dónde está el jugador ahora: mapa, zona, subzona, coordenadas y si está dentro de una instancia. Úsala cuando pregunten dónde están o para orientarles."

	descPersonaje = "Devuelve la ficha del personaje activo: nombre, clase, raza, nivel, experiencia, salud y estado (en combate, montado, descansando, muerto). Úsala para el contexto general del personaje."

	descMisiones = "Lista las misiones del registro del personaje activo con su estado, objetivos, quién las da y dónde se entregan, e indica cuál está rastreada. Incluye dónde se aceptan y entregan con coordenadas cuando el juego o Questie lo saben; el campo «fuente» dice de dónde sale cada dato. Úsala para saber qué tiene pendiente el jugador o en qué misión está."

	descMision = "Devuelve los detalles de una misión concreta por su número (quest_id): título, nivel, objetivos, descripción, NPC que la da, NPC de entrega y recompensas. Funciona aunque la misión no esté en el registro del jugador. Úsala cuando pregunten por una misión en particular."

	descObjetivo = "Devuelve el objetivo seleccionado ahora mismo (NPC o enemigo): nombre, nivel, si es hostil y lo que se sabe de él. Úsala cuando el jugador pregunte por lo que tiene delante o tiene seleccionado."

	descInventario = "Devuelve las bolsas del personaje con sus objetos y los espacios libres. Úsala para preguntas sobre los objetos que lleva o si le queda espacio."

	descEquipo = "Devuelve el equipo puesto, ordenado por ranura. Úsala para preguntas sobre su equipamiento o para recomendar mejoras."

	descGrupo = "Devuelve el grupo o la banda del personaje y sus miembros. Úsala cuando pregunten por su grupo o por jugar acompañado."

	descHechizos = "Devuelve el libro de hechizos del personaje. Úsala para preguntas sobre las habilidades que tiene disponibles."

	descTalentos = "Devuelve los talentos que el addon ha guardado (datos de mejor esfuerzo). Úsala para preguntas sobre su build o sobre qué talentos tiene."

	descProfesiones = "Devuelve las profesiones aprendidas y su nivel. Úsala para preguntas sobre oficios, recetas o recolección."

	descReputaciones = "Devuelve las reputaciones con las facciones (nivel y progreso). Úsala para saber cómo va el jugador con cada facción o qué le falta."

	descDinero = "Devuelve el dinero del personaje en oro, plata y cobre. Úsala para preguntas sobre cuánto tiene o si puede permitirse algo."

	descConocimiento = "Busca por nombre o palabra clave en el conocimiento aprendido de WoW Forever (misiones y NPCs que el addon ha visto en el juego). Úsala primero para preguntas sobre misiones o NPCs concretos; si no hay resultados, usa buscar_web."

	descNpcsMapa = "Lista los NPCs conocidos de un mapa (vendedores, entrenadores, dadores de misiones) con sus coordenadas. Sin map_id usa el mapa actual del jugador. Úsala para preguntas como dónde comprar algo o a quién acudir en la zona."

	descBuscarWeb = "Busca en las fuentes web de WoW Forever (wiki, guías y bases de datos de la versión Forever) cuando el conocimiento local no basta. Solo devuelve páginas de esas fuentes, nunca de Classic ni de Retail. Puede limitarse a un sitio concreto. Devuelve títulos, enlaces y extractos; usa leer_pagina para leer una página completa."

	descLeerPagina = "Lee el texto de una página de WoW Forever a partir de su URL, la que devolvió buscar_web. Úsala cuando el extracto no tenga la respuesta completa. Solo admite los sitios permitidos."

	descEntrenar = "Dice qué hechizos de clase puede aprender ahora con su coste, cuánto cuesta en total, si el jugador puede pagarlos y dónde está el entrenador de su clase cuando se conoce. Incluye también los hechizos del siguiente nivel. Úsala cuando pregunten qué entrenar, qué hechizos tiene disponibles o cuánto cuesta subir de nivel en hechizos."

	descRutaProfesion = "Devuelve cómo subir una profesión (SkillUpForever): el siguiente rango con sus recetas y materiales, la lista de la compra, y los entrenadores conocidos con su consejo de entrenamiento. Sin profesión devuelve todas. Úsala cuando pregunten cómo subir una profesión, qué comprar o qué recetas hacer."

	descResumenSesion = "Resume la sesión de juego en curso, o la última si no hay ninguna: duración, XP ganada, cambio de oro, misiones completadas y entregadas, zonas visitadas y muertes. Úsala cuando pidan un resumen de lo que han hecho o cómo ha ido la sesión."
)
