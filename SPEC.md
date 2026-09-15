# Database Dumper Server — Especificación

## Qué es

Versión web del CLI `magento-database-dumper`. Permite ejecutar dumps de bases de datos Magento remotas desde el browser, sin necesidad de usar la terminal.

## Qué hace

### Lista de servidores
- Muestra todos los servidores guardados en SQLite (importados de `dump.json` la primera vez)
- Por cada servidor se ve: ID, host SSH, base de datos, si está habilitado o no
- Se puede filtrar/buscar por ID o host

### Ejecución de dumps
- Botón "Dump" por servidor, con overrides opcionales para esa corrida: solo `core_config_data`, una partición de `only_tables`, o una lista ad-hoc de tablas
- Cada corrida es un job guardado en la tabla `jobs` (estado, log, archivos, fechas). El log se persiste cada 1s mientras corre, solo si cambió, y siempre al terminar
- La página del job muestra el log en vivo (polling HTMX cada 1s), permite frenarlo y descargar los `.sql.gz` resultantes mientras sigan en disco
- Si el server se reinicia con jobs en curso, quedan como `interrupted`. El proceso ssh muere con el server
- Progreso: mientras corre `mysqldump` el script remoto imprime el tamaño del `.sql.gz` cada 10s. Durante el `scp`, el server loguea bytes descargados sobre el total cada 10s
- Si `mysqldump` o `gzip` fallan, el script remoto borra el archivo parcial y el job falla, en vez de descargar un dump truncado

### Configuración
- Alta, edición, borrado y deshabilitado de servidores desde la UI
- Al arrancar, si la tabla `servers` está vacía, se importa `dump.json` del directorio de trabajo. Si `ignore_tables` u `only_tables` apuntan a un archivo, su contenido se vuelca a la lista

## Stack

- **Backend**: Go, `net/http`, `html/template`
- **Frontend**: HTMX para interactividad, sin JS custom
- **Base de datos**: SQLite (archivo local, sin servidor aparte)

## Base de datos

Las configuraciones de servidores se guardan en SQLite en lugar del `dump.json`.

Tabla `servers`:
- `id` — identificador único (ej: "powermusic-prod")
- `ssh_host` — host SSH del servidor remoto
- `ssh_pass` — contraseña SSH opcional
- `remote_env_path` — path al `env.php` de Magento en el servidor
- `local_path` — carpeta local donde se guarda el dump
- `enabled` — si el servidor está activo
- `ignore_tables` — tablas a ignorar (JSON array, admite wildcards en single table mode). No existe el concepto de archivo de exclusiones
- `only_tables` — texto con una tabla por línea. Líneas `#tag` o líneas vacías separan particiones, una por dump
- `with_core_config` — incluir `core_config_data`
- `only_core_config` — solo `core_config_data`
- `single_table_mode` — un dump y una descarga por tabla, con archivo de progreso para reanudar
- `enable_set_gtid_purged_off`, `skip_extended_insert`, `skip_add_locks`, `skip_disable_keys`, `skip_lock_tables`, `skip_add_drop_table` — flags de `mysqldump`
- `net_buffer_length` — tamaño de buffer MySQL
- `dump_client` — binario a usar en vez de `mysqldump`

## Despliegue

- El contenedor monta `~/.ssh` del usuario en `/host_ssh` y el entrypoint lo copia a `/root/.ssh` con permisos de root, porque ssh rechaza configs de otro dueño
- `sshpass` se instala al arrancar el contenedor
- `local_path` es un path dentro del contenedor. `docker-compose.yml` monta `~/dumps` en `/dumps`

## Usuarios y permisos

- Login con usuario y contraseña (bcrypt). Sesión en cookie `HttpOnly`, guardada en la tabla `sessions`, 30 días.
- Un usuario es **admin** o no. Admin ve todos los servers, crea servers y gestiona usuarios.
- Para los no-admin, el acceso es por server en la tabla `user_servers (user_id, server_id, role)`:
  - `editor`: edita la config del server, lo borra y genera dumps
  - `operator`: ve la config y genera dumps
  - sin fila: no ve el server
- Los jobs se ven solo si se tiene acceso al server correspondiente.
- Bootstrap: si no hay usuarios, se crea `admin` con `DUMPER_ADMIN_PASSWORD`, o con una contraseña aleatoria que se imprime en el log.
- CLI con el mismo binario, desde el directorio de `dumper.db`:
  - `server user list`
  - `server user create <usuario> [--admin]`
  - `server user passwd <usuario>` (invalida las sesiones del usuario)
  - `server user admin <usuario> on|off`
  - `server user delete <usuario>`
  - La contraseña sale de `DUMPER_ADMIN_PASSWORD` si está seteada, si no se pide por terminal. En dev: `task user -- passwd <usuario>`.

## Lo que NO hace (por ahora)

- No tiene protección CSRF. La cookie es `SameSite=Lax`, que cubre el caso común
- No programa dumps automáticos
