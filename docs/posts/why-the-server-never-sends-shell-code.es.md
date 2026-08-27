# Por qué mi servidor de alias nunca envía código de shell

Mantengo una herramienta pequeña que sincroniza alias de shell entre varias
máquinas. A mitad de camino tomé una decisión que condicionó todo lo que vino
después, y es la parte que vale la pena contar.

## El diseño obvio es un dotfile remoto

Si quieres que `gs` signifique `git status` en todas las máquinas que usas, el
camino más corto es guardar el texto en algún lugar central y que cada máquina
lo descargue:

```
alias gs='git status'
```

El servidor guarda esa línea. El cliente la baja. La shell la carga. Listo en
una tarde.

Ese diseño tiene una propiedad que dejó de gustarme en cuanto la miré de
frente: el servidor está escribiendo en tu shell. Los bytes que devuelva los
evalúa tu intérprete, en todas las máquinas, con tu usuario. Un error en el
servidor, un host comprometido, una equivocación al pegar un comando: todo eso
se convierte en ejecución de código en cada máquina registrada. El cliente no
tiene manera de distinguir un alias legítimo de un comando inyectado, porque
en esa capa son la misma cosa: texto para cargar.

Los dotfiles en Git tienen la misma forma, dicho sea de paso. La diferencia es
que uno normalmente lee sus propios commits.

## Lo que hice en su lugar

El servidor guarda lo que un alias *significa*, no cómo se escribe:

```
name:        gs
command:     git status
platforms:   [macos, linux]
shells:      [zsh, bash]
```

Ahí no hay sintaxis de shell por ningún lado. El cliente recibe esos campos y
produce la sintaxis él mismo, para la shell que está ejecutando realmente. zsh
y bash reciben `alias gs='git status'`. PowerShell recibe una función, porque
sus alias no aceptan argumentos y fingir lo contrario produce algo que parece
correcto y se comporta mal.

El escapado también ocurre en el cliente, lo cual importa más de lo que
parece. Un comando que contiene comillas lo escapa la máquina que sabe qué
shell va a leerlo, no un servidor adivinando.

## Convertirlo en un límite en vez de una promesa

Una decisión de diseño que solo vive en un documento es una decisión hasta que
alguien tiene prisa. Aquí la sostienen dos pruebas.

La primera prohíbe que el cliente importe código del servidor. No "no
debería": la prueba falla si el grafo de dependencias del binario cliente
contiene el almacenamiento, la API, el servidor, el paquete de sincronización
o el driver de SQLite. El cliente no puede desarrollar un camino de código que
hable con una base de datos, porque los paquetes que podrían hacerlo no son
alcanzables desde él.

La segunda me gusta más. Resuelve el mismo conjunto de alias dos veces: una
desde un archivo YAML local sin servidor de por medio, otra a través del
servidor HTTP real, renderiza ambas y compara la salida. Si esos dos caminos
llegan a diferir en un solo byte, la prueba falla.

Esa segunda prueba es lo que hace la afirmación verificable en vez de
retórica. "El servidor no envía código de shell" es fácil de decir. "El
servidor no puede influir en los bytes generados, y aquí está la prueba que
falla si empieza a hacerlo" es una afirmación de otro tipo.

También trajo algo que no había planeado: la herramienta funciona sin ningún
servidor. Apúntala a un archivo local y se comporta igual. El servidor es una
forma opcional de distribuir los datos, no lo que hace que la herramienta
funcione.

## Lo que cuesta

La honestidad exige la otra columna.

El servidor no puede enviar nada que el cliente no sepa renderizar ya. Añadir
una construcción implica publicar una versión del cliente, no un cambio en el
servidor. Cada máquina tiene que actualizarse antes de poder usarla. Eso es
más lento que editar un archivo en el servidor, y hay días en que resulta
molesto.

Creo que es el intercambio correcto. El conjunto de cosas que quiero enviar a
todas mis máquinas es pequeño, y no incluye código arbitrario.

## La versión general

Si construyes cualquier cosa que distribuya configuración a máquinas, la
pregunta merece hacerse directamente: ¿mi contenido se *interpreta* del otro
lado, o se *lee*?

Si se interpreta, tu canal de configuración es un canal de ejecución de
código, y merece el escrutinio que le darías a uno. Enviar datos y renderizar
localmente da más trabajo al principio y muchas menos preocupaciones después.

---

La herramienta es [AliasDeck](https://github.com/angeltonio/aliasdeck): Go,
autoalojada, software libre. No vendo nada; si el diseño está mal, prefiero
enterarme ahora.
