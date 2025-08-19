package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Configuration
const (
	AllowTestMenu = true
	DefaultPort   = ":8000"
)

// NavicatTunnel handles the HTTP tunnel functionality
type NavicatTunnel struct{}

// NewNavicatTunnel creates a new tunnel instance
func NewNavicatTunnel() *NavicatTunnel {
	return &NavicatTunnel{}
}

// GetLongBinary converts uint32 to 4-byte big-endian
func (nt *NavicatTunnel) GetLongBinary(num uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, num)
	return buf
}

// GetShortBinary converts uint16 to 2-byte big-endian
func (nt *NavicatTunnel) GetShortBinary(num uint16) []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, num)
	return buf
}

// GetDummy generates null bytes
func (nt *NavicatTunnel) GetDummy(count int) []byte {
	return make([]byte, count)
}

// GetBlock encodes string with length prefix
func (nt *NavicatTunnel) GetBlock(val string) []byte {
	data := []byte(val)
	length := len(data)
	
	if length < 254 {
		result := make([]byte, 1+length)
		result[0] = byte(length)
		copy(result[1:], data)
		return result
	} else {
		result := make([]byte, 5+length)
		result[0] = 0xFE
		binary.BigEndian.PutUint32(result[1:5], uint32(length))
		copy(result[5:], data)
		return result
	}
}

// EchoHeader generates response header
func (nt *NavicatTunnel) EchoHeader(errno uint32) []byte {
	var buf bytes.Buffer
	buf.Write(nt.GetLongBinary(1111))
	buf.Write(nt.GetShortBinary(202))
	buf.Write(nt.GetLongBinary(errno))
	buf.Write(nt.GetDummy(6))
	return buf.Bytes()
}

// EchoConnInfo generates connection information
func (nt *NavicatTunnel) EchoConnInfo(db *sql.DB) []byte {
	var buf bytes.Buffer
	
	// Get server version
	var version string
	err := db.QueryRow("SELECT VERSION()").Scan(&version)
	if err != nil {
		version = "Unknown"
	}
	
	// Mock connection info (Go's sql package doesn't provide detailed connection info)
	hostInfo := "MySQL via TCP/IP"
	protoInfo := "10"
	
	buf.Write(nt.GetBlock(hostInfo))
	buf.Write(nt.GetBlock(protoInfo))
	buf.Write(nt.GetBlock(version))
	
	return buf.Bytes()
}

// EchoResultSetHeader generates result set header
func (nt *NavicatTunnel) EchoResultSetHeader(errno, affectedRows, insertID, numFields, numRows uint32) []byte {
	var buf bytes.Buffer
	buf.Write(nt.GetLongBinary(errno))
	buf.Write(nt.GetLongBinary(affectedRows))
	buf.Write(nt.GetLongBinary(insertID))
	buf.Write(nt.GetLongBinary(numFields))
	buf.Write(nt.GetLongBinary(numRows))
	buf.Write(nt.GetDummy(12))
	return buf.Bytes()
}

// MySQLFieldType represents MySQL field types
type MySQLFieldType int

const (
	MYSQL_TYPE_DECIMAL MySQLFieldType = iota
	MYSQL_TYPE_TINY
	MYSQL_TYPE_SHORT
	MYSQL_TYPE_LONG
	MYSQL_TYPE_FLOAT
	MYSQL_TYPE_DOUBLE
	MYSQL_TYPE_NULL
	MYSQL_TYPE_TIMESTAMP
	MYSQL_TYPE_LONGLONG
	MYSQL_TYPE_INT24
	MYSQL_TYPE_DATE
	MYSQL_TYPE_TIME
	MYSQL_TYPE_DATETIME
	MYSQL_TYPE_YEAR
	MYSQL_TYPE_NEWDATE
	MYSQL_TYPE_VARCHAR     = 15
	MYSQL_TYPE_BIT         = 16
	MYSQL_TYPE_JSON        = 245
	MYSQL_TYPE_NEWDECIMAL  = 246
	MYSQL_TYPE_ENUM        = 247
	MYSQL_TYPE_SET         = 248
	MYSQL_TYPE_TINY_BLOB   = 249
	MYSQL_TYPE_MEDIUM_BLOB = 250
	MYSQL_TYPE_LONG_BLOB   = 251
	MYSQL_TYPE_BLOB        = 252
	MYSQL_TYPE_VAR_STRING  = 253
	MYSQL_TYPE_STRING      = 254
	MYSQL_TYPE_GEOMETRY    = 255
)

// MapGoTypeToMySQL maps Go types to MySQL field types
func (nt *NavicatTunnel) MapGoTypeToMySQL(goType reflect.Type) MySQLFieldType {
	switch goType.Kind() {
	case reflect.Bool:
		return MYSQL_TYPE_TINY
	case reflect.Int8:
		return MYSQL_TYPE_TINY
	case reflect.Int16:
		return MYSQL_TYPE_SHORT
	case reflect.Int32:
		return MYSQL_TYPE_LONG
	case reflect.Int64, reflect.Int:
		return MYSQL_TYPE_LONGLONG
	case reflect.Float32:
		return MYSQL_TYPE_FLOAT
	case reflect.Float64:
		return MYSQL_TYPE_DOUBLE
	case reflect.String:
		return MYSQL_TYPE_VAR_STRING
	default:
		return MYSQL_TYPE_VAR_STRING
	}
}

// EchoFieldsHeader generates fields header information
func (nt *NavicatTunnel) EchoFieldsHeader(columns []string, types []*sql.ColumnType) []byte {
	var buf bytes.Buffer
	
	for i, column := range columns {
		// Field name
		buf.Write(nt.GetBlock(column))
		// Table name (not available in Go's sql package)
		buf.Write(nt.GetBlock(""))
		
		// Field type - try to determine from column type
		var fieldType MySQLFieldType = MYSQL_TYPE_VAR_STRING
		var length uint32 = 255
		var flags uint32 = 0
		
		if i < len(types) && types[i] != nil {
			// Try to get length information
			if l, ok := types[i].Length(); ok {
				length = uint32(l)
			}
			
			// Try to determine type from database type name
			dbType := strings.ToUpper(types[i].DatabaseTypeName())
			fieldType = nt.GetMySQLTypeFromName(dbType)
			
			// Check nullable
			if nullable, ok := types[i].Nullable(); ok && !nullable {
				flags |= 1 // NOT_NULL flag
			}
		}
		
		buf.Write(nt.GetLongBinary(uint32(fieldType)))
		buf.Write(nt.GetLongBinary(flags))
		buf.Write(nt.GetLongBinary(length))
	}
	
	return buf.Bytes()
}

// GetMySQLTypeFromName maps database type name to MySQL type
func (nt *NavicatTunnel) GetMySQLTypeFromName(typeName string) MySQLFieldType {
	switch {
	case strings.Contains(typeName, "TINYINT"):
		return MYSQL_TYPE_TINY
	case strings.Contains(typeName, "SMALLINT"):
		return MYSQL_TYPE_SHORT
	case strings.Contains(typeName, "MEDIUMINT"):
		return MYSQL_TYPE_INT24
	case strings.Contains(typeName, "BIGINT"):
		return MYSQL_TYPE_LONGLONG
	case strings.Contains(typeName, "INT"):
		return MYSQL_TYPE_LONG
	case strings.Contains(typeName, "FLOAT"):
		return MYSQL_TYPE_FLOAT
	case strings.Contains(typeName, "DOUBLE"):
		return MYSQL_TYPE_DOUBLE
	case strings.Contains(typeName, "DECIMAL"):
		return MYSQL_TYPE_NEWDECIMAL
	case strings.Contains(typeName, "DATE"):
		return MYSQL_TYPE_DATE
	case strings.Contains(typeName, "TIME"):
		if strings.Contains(typeName, "DATETIME") || strings.Contains(typeName, "TIMESTAMP") {
			return MYSQL_TYPE_DATETIME
		}
		return MYSQL_TYPE_TIME
	case strings.Contains(typeName, "YEAR"):
		return MYSQL_TYPE_YEAR
	case strings.Contains(typeName, "CHAR"), strings.Contains(typeName, "VARCHAR"):
		return MYSQL_TYPE_VAR_STRING
	case strings.Contains(typeName, "TEXT"):
		return MYSQL_TYPE_BLOB
	case strings.Contains(typeName, "BLOB"):
		return MYSQL_TYPE_BLOB
	case strings.Contains(typeName, "JSON"):
		return MYSQL_TYPE_JSON
	default:
		return MYSQL_TYPE_VAR_STRING
	}
}

// EchoData generates result data
func (nt *NavicatTunnel) EchoData(rows *sql.Rows, numFields int) []byte {
	var buf bytes.Buffer
	
	// Create slice to hold column values
	columns := make([]interface{}, numFields)
	columnPointers := make([]interface{}, numFields)
	
	for i := range columns {
		columnPointers[i] = &columns[i]
	}
	
	for rows.Next() {
		err := rows.Scan(columnPointers...)
		if err != nil {
			continue
		}
		
		for _, col := range columns {
			if col == nil {
				buf.Write([]byte{0xFF})
			} else {
				var value string
				switch v := col.(type) {
				case string:
					value = v
				case []byte:
					value = string(v)
				case int64:
					value = strconv.FormatInt(v, 10)
				case float64:
					value = strconv.FormatFloat(v, 'f', -1, 64)
				case bool:
					if v {
						value = "1"
					} else {
						value = "0"
					}
				case time.Time:
					value = v.Format("2006-01-02 15:04:05")
				default:
					value = fmt.Sprintf("%v", v)
				}
				buf.Write(nt.GetBlock(value))
			}
		}
	}
	
	return buf.Bytes()
}

// HandleConnectionTest handles connection testing
func (nt *NavicatTunnel) HandleConnectionTest(params url.Values) []byte {
	host := params.Get("host")
	if host == "" {
		host = "localhost"
	}
	
	port := params.Get("port")
	if port == "" {
		port = "3306"
	}
	
	user := params.Get("login")
	password := params.Get("password")
	database := params.Get("db")
	
	// Build DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true",
		user, password, host, port, database)
	
	// Test connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nt.createErrorResponse(2000, err.Error())
	}
	defer db.Close()
	
	// Test actual connection
	err = db.Ping()
	if err != nil {
		return nt.createErrorResponse(2000, err.Error())
	}
	
	// Success - return connection info
	var buf bytes.Buffer
	buf.Write(nt.EchoHeader(0))
	buf.Write(nt.EchoConnInfo(db))
	
	return buf.Bytes()
}

// HandleQueryExecution handles query execution
func (nt *NavicatTunnel) HandleQueryExecution(params url.Values) []byte {
	host := params.Get("host")
	if host == "" {
		host = "localhost"
	}
	
	port := params.Get("port")
	if port == "" {
		port = "3306"
	}
	
	user := params.Get("login")
	password := params.Get("password")
	database := params.Get("db")
	
	// Handle base64 encoding
	queries := params["q"]
	if params.Get("encodeBase64") == "1" {
		for i, query := range queries {
			if decoded, err := base64.StdEncoding.DecodeString(query); err == nil {
				queries[i] = string(decoded)
			}
		}
	}
	
	// Build DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true",
		user, password, host, port, database)
	
	// Open connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nt.createErrorResponse(2000, err.Error())
	}
	defer db.Close()
	
	// Test connection
	err = db.Ping()
	if err != nil {
		return nt.createErrorResponse(2000, err.Error())
	}
	
	var buf bytes.Buffer
	buf.Write(nt.EchoHeader(0))
	
	// Execute queries
	for i, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		
		// Execute query
		var errno uint32 = 0
		var affectedRows, insertID, numFields, numRows uint32 = 0, 0, 0, 0
		var errorMsg string
		
		// Determine if it's a SELECT query
		isSelect := strings.HasPrefix(strings.ToUpper(query), "SELECT")
		
		if isSelect {
			// Handle SELECT query
			rows, err := db.Query(query)
			if err != nil {
				errno = 1000
				errorMsg = err.Error()
			} else {
				defer rows.Close()
				
				// Get column information
				columns, err := rows.Columns()
				if err != nil {
					errno = 1000
					errorMsg = err.Error()
				} else {
					numFields = uint32(len(columns))
					
					// Count rows by scanning through them
					rowCount := uint32(0)
					var rowsData bytes.Buffer
					
					// Get column types
					columnTypes, _ := rows.ColumnTypes()
					
					// Store field header
					fieldsHeader := nt.EchoFieldsHeader(columns, columnTypes)
					
					// Count and store data
					columnValues := make([]interface{}, len(columns))
					columnPointers := make([]interface{}, len(columns))
					for i := range columnValues {
						columnPointers[i] = &columnValues[i]
					}
					
					for rows.Next() {
						rowCount++
						err := rows.Scan(columnPointers...)
						if err != nil {
							continue
						}
						
						for _, col := range columnValues {
							if col == nil {
								rowsData.Write([]byte{0xFF})
							} else {
								var value string
								switch v := col.(type) {
								case string:
									value = v
								case []byte:
									value = string(v)
								case int64:
									value = strconv.FormatInt(v, 10)
								case float64:
									value = strconv.FormatFloat(v, 'f', -1, 64)
								case bool:
									if v {
										value = "1"
									} else {
										value = "0"
									}
								case time.Time:
									value = v.Format("2006-01-02 15:04:05")
								default:
									value = fmt.Sprintf("%v", v)
								}
								rowsData.Write(nt.GetBlock(value))
							}
						}
					}
					
					numRows = rowCount
					
					// Write result set header
					buf.Write(nt.EchoResultSetHeader(errno, affectedRows, insertID, numFields, numRows))
					
					if errno == 0 {
						// Write fields header and data
						buf.Write(fieldsHeader)
						buf.Write(rowsData.Bytes())
					} else {
						buf.Write(nt.GetBlock(errorMsg))
					}
				}
			}
		} else {
			// Handle non-SELECT query
			result, err := db.Exec(query)
			if err != nil {
				errno = 1000
				errorMsg = err.Error()
			} else {
				if affected, err := result.RowsAffected(); err == nil {
					affectedRows = uint32(affected)
				}
				if lastID, err := result.LastInsertId(); err == nil {
					insertID = uint32(lastID)
				}
			}
			
			// Write result set header
			buf.Write(nt.EchoResultSetHeader(errno, affectedRows, insertID, numFields, numRows))
			
			if errno > 0 {
				buf.Write(nt.GetBlock(errorMsg))
			} else {
				// Add info block
				info := fmt.Sprintf("Rows affected: %d", affectedRows)
				buf.Write(nt.GetBlock(info))
			}
		}
		
		// Add query separator
		if i < len(queries)-1 {
			buf.Write([]byte{0x01})
		} else {
			buf.Write([]byte{0x00})
		}
	}
	
	return buf.Bytes()
}

// createErrorResponse creates an error response
func (nt *NavicatTunnel) createErrorResponse(errno uint32, message string) []byte {
	var buf bytes.Buffer
	buf.Write(nt.EchoHeader(errno))
	buf.Write(nt.GetBlock(message))
	return buf.Bytes()
}

// GetSystemTestHTML generates system test HTML
func (nt *NavicatTunnel) GetSystemTestHTML() string {
	goVersion := runtime.Version()
	platform := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
	
	// Test MySQL driver
	mysqlAvailable := "Yes"
	mysqlClass := "TestSucc"
	if _, err := sql.Open("mysql", "test:test@tcp(localhost:3306)/test"); err != nil {
		mysqlAvailable = "No"
		mysqlClass = "TestFail"
	}
	
	return fmt.Sprintf(`
		<tr><td class="TestDesc">Go version</td><td class="TestSucc">%s</td></tr>
		<tr><td class="TestDesc">Platform</td><td class="TestSucc">%s</td></tr>
		<tr><td class="TestDesc">MySQL driver available</td><td class="%s">%s</td></tr>
	`, goVersion, platform, mysqlClass, mysqlAvailable)
}

// GetTestPageHTML generates the test page HTML
func (nt *NavicatTunnel) GetTestPageHTML() string {
	systemTests := nt.GetSystemTestHTML()
	
	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>Navicat HTTP Tunnel Tester (Go Version)</title>
    <meta charset="UTF-8">
    <style type="text/css">
        body{
            margin: 30px;
            font-family: Tahoma, sans-serif;
            font-weight: normal;
            font-size: 14px;
            color: #222222;
        }
        table{
            width: 100%;
            border: 0px;
        }
        input{
            font-family: Tahoma, sans-serif;
            border-style: solid;
            border-color: #666666;
            border-width: 1px;
        }
        fieldset{
            border-style: solid;
            border-color: #666666;
            border-width: 1px;
        }
        .Title1{
            font-size: 30px;
            color: #003366;
        }
        .Title2{
            font-size: 10px;
            color: #999966;
        }
        .TestDesc{
            width: 70%;
        }
        .TestSucc{
            color: #00BB00;
        }
        .TestFail{
            color: #DD0000;
        }
        #page{
            max-width: 42em;
            min-width: 36em;
            border-width: 0px;
            margin: auto auto;
        }
        #host{
            width: 300px;
        }
        #port{
            width: 75px;
        }
        #login, #password, #db{
            width: 150px;
        }
        #Copyright{
            text-align: right;
            font-size: 10px;
            color: #888888;
        }
    </style>
    <script type="text/javascript">
    function setText(element, text, succ){
        element.className = (succ)?"TestSucc":"TestFail";
        element.innerHTML = text;
    }
    function getByteAt(str, offset){
        return str.charCodeAt(offset) & 0xff;
    }
    function getIntAt(binStr, offset){
        return (getByteAt(binStr, offset) << 24)+
            (getByteAt(binStr, offset+1) << 16)+
            (getByteAt(binStr, offset+2) << 8)+
            (getByteAt(binStr, offset+3) >>> 0);
    }
    function getBlockStr(binStr, offset){
        if (getByteAt(binStr, offset) < 254)
            return binStr.substring(offset+1, offset+1+binStr.charCodeAt(offset));
        else
            return binStr.substring(offset+5, offset+5+getIntAt(binStr, offset+1));
    }
    function doServerTest(){
        var xmlhttp = new XMLHttpRequest();
        
        xmlhttp.onreadystatechange=function(){
            var outputDiv = document.getElementById("ServerTest");
            if (xmlhttp.readyState == 4){
                if (xmlhttp.status == 200){
                    var errno = getIntAt(xmlhttp.responseText, 6);
                    if (errno == 0)
                        setText(outputDiv, "Connection Success!", true);
                    else
                        setText(outputDiv, parseInt(errno)+" - "+getBlockStr(xmlhttp.responseText, 16), false);
                }else
                    setText(outputDiv, "HTTP Error - "+xmlhttp.status, false);
            }
        }
        
        var params = "";
        var form = document.getElementById("TestServerForm");
        for (var i=0; i<form.elements.length; i++){
            if (i>0) params += "&";
            params += form.elements[i].id+"="+encodeURIComponent(form.elements[i].value);
        }
        
        document.getElementById("ServerTest").className = "";
        document.getElementById("ServerTest").innerHTML = "Connecting...";
        xmlhttp.open("POST", "", true);
        xmlhttp.setRequestHeader("Content-type", "application/x-www-form-urlencoded");
        xmlhttp.send(params);
    }
    </script>
</head>
<body>
<div id="page">
<p>
    <span class="Title1">Navicat&trade;</span><br>
    <span class="Title2">The gateway to your database! (Go Version)</span>
</p>
<fieldset>
    <legend>System Environment Test</legend>
    <table>
        {{.SystemTests}}
    </table>
</fieldset>
<br>
<fieldset>
    <legend>Server Test</legend>
    <form id="TestServerForm" action="#" onSubmit="return false;">
    <input type="hidden" id="actn" value="C">
    <table>
        <tr><td width="35%">Hostname/IP Address:</td><td><input type="text" id="host" placeholder="localhost"></td></tr>
        <tr><td>Port:</td><td><input type="text" id="port" placeholder="3306"></td></tr>
        <tr><td>Username:</td><td><input type="text" id="login" placeholder="root"></td></tr>
        <tr><td>Password:</td><td><input type="password" id="password" placeholder=""></td></tr>
        <tr><td>Database:</td><td><input type="text" id="db" placeholder=""></td></tr>
        <tr><td></td><td><br><input type="submit" value="Test Connection" onClick="doServerTest()"></td></tr>
    </table>
    </form>
    <div id="ServerTest"><br></div>
</fieldset>
<p id="Copyright">Copyright &copy; PremiumSoft &trade; CyberTech Ltd. All Rights Reserved.</p>
</div>
</body>
</html>`

	t, _ := template.New("test").Parse(tmpl)
	var buf bytes.Buffer
	t.Execute(&buf, struct {
		SystemTests template.HTML
	}{
		SystemTests: template.HTML(systemTests),
	})
	
	return buf.String()
}

// HTTP handler
func (nt *NavicatTunnel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// Parse form data
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		
		// Check required parameters
		action := r.Form.Get("actn")
		host := r.Form.Get("host")
		port := r.Form.Get("port")
		login := r.Form.Get("login")
		
		if action == "" || host == "" || port == "" || login == "" {
			if !AllowTestMenu {
				w.Header().Set("Content-Type", "text/plain; charset=x-user-defined")
				response := nt.createErrorResponse(202, "invalid parameters")
				w.Write(response)
				return
			} else {
				// Show test page
				w.Header().Set("Content-Type", "text/html; charset=UTF-8")
				html := nt.GetTestPageHTML()
				w.Write([]byte(html))
				return
			}
		}
		
		// Handle actions
		var response []byte
		
		switch action {
		case "C":
			// Connection test
			response = nt.HandleConnectionTest(r.Form)
		case "Q":
			// Query execution
			response = nt.HandleQueryExecution(r.Form)
		default:
			response = nt.createErrorResponse(202, "invalid action")
		}
		
		w.Header().Set("Content-Type", "text/plain; charset=x-user-defined")
		w.Write(response)
		
	} else {
		// GET request - show test page if allowed
		if AllowTestMenu {
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			html := nt.GetTestPageHTML()
			w.Write([]byte(html))
		} else {
			http.Error(w, "Access denied", http.StatusForbidden)
		}
	}
}

func main() {
	tunnel := NewNavicatTunnel()
	
	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultPort
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	
	fmt.Printf("Starting Navicat HTTP Tunnel (Go) on port %s\n", port)
	fmt.Printf("Access: http://localhost%s\n", port)
	
	// Setup HTTP server
	http.Handle("/", tunnel)
	
	// Start server
	log.Fatal(http.ListenAndServe(port, nil))
}