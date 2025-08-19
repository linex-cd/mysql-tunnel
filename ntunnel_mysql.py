#!/usr/bin/env python3
"""
Navicat HTTP Tunnel - Python Version
Compatible with Python 3.6+
Requires: pymysql, flask (or any WSGI framework)
"""

import struct
import base64
import json
import sys
import platform
from urllib.parse import parse_qs
from wsgiref.simple_server import make_server
import os

# Try to import pymysql
try:
    import pymysql
    MYSQL_AVAILABLE = True
except ImportError:
    MYSQL_AVAILABLE = False
    pymysql = None

# Configuration
ALLOW_TEST_MENU = True

class NavicatTunnel:
    def __init__(self):
        self.use_mysql = MYSQL_AVAILABLE
        
    def get_long_binary(self, num):
        """Pack 32-bit integer in network byte order"""
        return struct.pack('>I', num)
    
    def get_short_binary(self, num):
        """Pack 16-bit integer in network byte order"""
        return struct.pack('>H', num)
    
    def get_dummy(self, count):
        """Generate null bytes"""
        return b'\x00' * count
    
    def get_block(self, val):
        """Encode string with length prefix"""
        if val is None:
            val = ""
        if isinstance(val, str):
            val = val.encode('utf-8')
        
        length = len(val)
        if length < 254:
            return bytes([length]) + val
        else:
            return b'\xFE' + self.get_long_binary(length) + val
    
    def echo_header(self, errno):
        """Echo response header"""
        response = self.get_long_binary(1111)
        response += self.get_short_binary(202)
        response += self.get_long_binary(errno)
        response += self.get_dummy(6)
        return response
    
    def echo_conn_info(self, connection):
        """Echo connection information"""
        try:
            # Get connection info
            connection.ping(reconnect=True)
            cursor = connection.cursor()
            
            # Get server info
            cursor.execute("SELECT VERSION()")
            server_version = cursor.fetchone()[0]
            
            # Mock host and protocol info (pymysql doesn't provide direct access)
            host_info = f"MySQL via TCP/IP"
            proto_info = "10"  # MySQL protocol version
            
            response = self.get_block(host_info)
            response += self.get_block(proto_info)
            response += self.get_block(server_version)
            
            cursor.close()
            return response
            
        except Exception as e:
            return self.get_block(str(e))
    
    def echo_result_set_header(self, errno, affected_rows, insert_id, num_fields, num_rows):
        """Echo result set header"""
        response = self.get_long_binary(errno)
        response += self.get_long_binary(affected_rows)
        response += self.get_long_binary(insert_id)
        response += self.get_long_binary(num_fields)
        response += self.get_long_binary(num_rows)
        response += self.get_dummy(12)
        return response
    
    def echo_fields_header(self, cursor):
        """Echo fields header information"""
        response = b""
        if cursor.description:
            for field in cursor.description:
                name = field[0] or ""
                table = ""  # pymysql doesn't provide table info easily
                
                # Map Python DB-API type to MySQL type
                field_type = field[1]
                type_code = self.map_python_type_to_mysql(field_type)
                
                length = field[2] or 0
                flags = 0  # Basic flags, pymysql doesn't provide detailed flags
                
                response += self.get_block(name)
                response += self.get_block(table)
                response += self.get_long_binary(type_code)
                response += self.get_long_binary(flags)
                response += self.get_long_binary(length)
        
        return response
    
    def map_python_type_to_mysql(self, python_type):
        """Map Python DB-API types to MySQL field types"""
        # This is a basic mapping - you might need to adjust based on your needs
        type_mapping = {
            pymysql.FIELD_TYPE.TINY: 1,
            pymysql.FIELD_TYPE.SHORT: 2,
            pymysql.FIELD_TYPE.LONG: 3,
            pymysql.FIELD_TYPE.FLOAT: 4,
            pymysql.FIELD_TYPE.DOUBLE: 5,
            pymysql.FIELD_TYPE.NULL: 6,
            pymysql.FIELD_TYPE.TIMESTAMP: 7,
            pymysql.FIELD_TYPE.LONGLONG: 8,
            pymysql.FIELD_TYPE.INT24: 9,
            pymysql.FIELD_TYPE.DATE: 10,
            pymysql.FIELD_TYPE.TIME: 11,
            pymysql.FIELD_TYPE.DATETIME: 12,
            pymysql.FIELD_TYPE.YEAR: 13,
            pymysql.FIELD_TYPE.VARCHAR: 253,
            pymysql.FIELD_TYPE.VAR_STRING: 253,
            pymysql.FIELD_TYPE.STRING: 254,
        }
        return type_mapping.get(python_type, 253)  # Default to VARCHAR
    
    def echo_data(self, cursor, num_fields, num_rows):
        """Echo result data"""
        response = b""
        if cursor.description:
            rows = cursor.fetchall()
            for row in rows:
                row_data = b""
                for value in row:
                    if value is None:
                        row_data += b'\xFF'
                    else:
                        if isinstance(value, (int, float)):
                            value = str(value)
                        row_data += self.get_block(value)
                response += row_data
        return response
    
    def handle_connection_test(self, params):
        """Handle connection test"""
        if not MYSQL_AVAILABLE:
            return self.echo_header(203) + self.get_block("MySQL not supported - pymysql not installed")
        
        try:
            host = params.get('host', ['localhost'])[0]
            port = int(params.get('port', [3306])[0]) if params.get('port', [''])[0] else 3306
            user = params.get('login', [''])[0]
            password = params.get('password', [''])[0]
            database = params.get('db', [''])[0] if params.get('db', [''])[0] else None
            
            # Create connection
            connection = pymysql.connect(
                host=host,
                port=port,
                user=user,
                password=password,
                database=database,
                charset='utf8mb4'
            )
            
            response = self.echo_header(0)  # Success
            response += self.echo_conn_info(connection)
            
            connection.close()
            return response
            
        except pymysql.Error as e:
            error_code = e.args[0] if e.args else 2000
            error_msg = str(e.args[1]) if len(e.args) > 1 else str(e)
            return self.echo_header(error_code) + self.get_block(error_msg)
        except Exception as e:
            return self.echo_header(2000) + self.get_block(str(e))
    
    def handle_query_execution(self, params):
        """Handle query execution"""
        if not MYSQL_AVAILABLE:
            return self.echo_header(203) + self.get_block("MySQL not supported - pymysql not installed")
        
        try:
            host = params.get('host', ['localhost'])[0]
            port = int(params.get('port', [3306])[0]) if params.get('port', [''])[0] else 3306
            user = params.get('login', [''])[0]
            password = params.get('password', [''])[0]
            database = params.get('db', [''])[0] if params.get('db', [''])[0] else None
            
            # Handle base64 encoding
            queries = params.get('q', [])
            if params.get('encodeBase64', ['0'])[0] == '1':
                queries = [base64.b64decode(q).decode('utf-8') for q in queries]
            
            # Create connection
            connection = pymysql.connect(
                host=host,
                port=port,
                user=user,
                password=password,
                database=database,
                charset='utf8mb4'
            )
            
            response = self.echo_header(0)  # Connection success
            
            # Execute queries
            for i, query in enumerate(queries):
                if not query.strip():
                    continue
                
                cursor = connection.cursor()
                try:
                    cursor.execute(query)
                    
                    # Get query results
                    errno = 0
                    affected_rows = cursor.rowcount if cursor.rowcount >= 0 else 0
                    insert_id = connection.insert_id()
                    num_fields = len(cursor.description) if cursor.description else 0
                    num_rows = cursor.rowcount if cursor.description else 0
                    
                    # For SELECT queries, get actual row count
                    if cursor.description and cursor.rowcount > 0:
                        # Store results to get row count
                        results = cursor.fetchall()
                        num_rows = len(results)
                        # Reset cursor with results
                        cursor.close()
                        cursor = connection.cursor()
                        cursor.execute(query)
                    
                    response += self.echo_result_set_header(errno, affected_rows, insert_id, num_fields, num_rows)
                    
                    if num_fields > 0:
                        response += self.echo_fields_header(cursor)
                        response += self.echo_data(cursor, num_fields, num_rows)
                    else:
                        # For non-SELECT queries, add info block
                        info = f"Rows matched: {affected_rows}"
                        response += self.get_block(info)
                    
                except pymysql.Error as e:
                    errno = e.args[0] if e.args else 1000
                    error_msg = str(e.args[1]) if len(e.args) > 1 else str(e)
                    
                    response += self.echo_result_set_header(errno, 0, 0, 0, 0)
                    response += self.get_block(error_msg)
                
                finally:
                    cursor.close()
                
                # Add query separator
                if i < len(queries) - 1:
                    response += b'\x01'
                else:
                    response += b'\x00'
            
            connection.close()
            return response
            
        except Exception as e:
            return self.echo_header(2000) + self.get_block(str(e))
    
    def get_system_test_html(self):
        """Generate system test HTML"""
        python_version = sys.version
        mysql_available = "Yes" if MYSQL_AVAILABLE else "No"
        mysql_class = "TestSucc" if MYSQL_AVAILABLE else "TestFail"
        platform_info = platform.platform()
        
        return f'''
        <tr><td class="TestDesc">Python version</td><td class="TestSucc">{python_version}</td></tr>
        <tr><td class="TestDesc">Platform</td><td class="TestSucc">{platform_info}</td></tr>
        <tr><td class="TestDesc">PyMySQL available</td><td class="{mysql_class}">{mysql_available}</td></tr>
        '''
    
    def get_test_page_html(self):
        """Generate test page HTML"""
        system_tests = self.get_system_test_html()
        
        return f'''<!DOCTYPE html>
<html>
<head>
    <title>Navicat HTTP Tunnel Tester (Python Version)</title>
    <meta charset="UTF-8">
    <style type="text/css">
        body{{
            margin: 30px;
            font-family: Tahoma, sans-serif;
            font-weight: normal;
            font-size: 14px;
            color: #222222;
        }}
        table{{
            width: 100%;
            border: 0px;
        }}
        input{{
            font-family: Tahoma, sans-serif;
            border-style: solid;
            border-color: #666666;
            border-width: 1px;
        }}
        fieldset{{
            border-style: solid;
            border-color: #666666;
            border-width: 1px;
        }}
        .Title1{{
            font-size: 30px;
            color: #003366;
        }}
        .Title2{{
            font-size: 10px;
            color: #999966;
        }}
        .TestDesc{{
            width: 70%;
        }}
        .TestSucc{{
            color: #00BB00;
        }}
        .TestFail{{
            color: #DD0000;
        }}
        #page{{
            max-width: 42em;
            min-width: 36em;
            border-width: 0px;
            margin: auto auto;
        }}
        #host{{
            width: 300px;
        }}
        #port{{
            width: 75px;
        }}
        #login, #password, #db{{
            width: 150px;
        }}
        #Copyright{{
            text-align: right;
            font-size: 10px;
            color: #888888;
        }}
    </style>
    <script type="text/javascript">
    function setText(element, text, succ){{
        element.className = (succ)?"TestSucc":"TestFail";
        element.innerHTML = text;
    }}
    function getByteAt(str, offset){{
        return str.charCodeAt(offset) & 0xff;
    }}
    function getIntAt(binStr, offset){{
        return (getByteAt(binStr, offset) << 24)+
            (getByteAt(binStr, offset+1) << 16)+
            (getByteAt(binStr, offset+2) << 8)+
            (getByteAt(binStr, offset+3) >>> 0);
    }}
    function getBlockStr(binStr, offset){{
        if (getByteAt(binStr, offset) < 254)
            return binStr.substring(offset+1, offset+1+binStr.charCodeAt(offset));
        else
            return binStr.substring(offset+5, offset+5+getIntAt(binStr, offset+1));
    }}
    function doServerTest(){{
        var xmlhttp = new XMLHttpRequest();
        
        xmlhttp.onreadystatechange=function(){{
            var outputDiv = document.getElementById("ServerTest");
            if (xmlhttp.readyState == 4){{
                if (xmlhttp.status == 200){{
                    var errno = getIntAt(xmlhttp.responseText, 6);
                    if (errno == 0)
                        setText(outputDiv, "Connection Success!", true);
                    else
                        setText(outputDiv, parseInt(errno)+" - "+getBlockStr(xmlhttp.responseText, 16), false);
                }}else
                    setText(outputDiv, "HTTP Error - "+xmlhttp.status, false);
            }}
        }}
        
        var params = "";
        var form = document.getElementById("TestServerForm");
        for (var i=0; i<form.elements.length; i++){{
            if (i>0) params += "&";
            params += form.elements[i].id+"="+encodeURIComponent(form.elements[i].value);
        }}
        
        document.getElementById("ServerTest").className = "";
        document.getElementById("ServerTest").innerHTML = "Connecting...";
        xmlhttp.open("POST", "", true);
        xmlhttp.setRequestHeader("Content-type", "application/x-www-form-urlencoded");
        xmlhttp.send(params);
    }}
    </script>
</head>
<body>
<div id="page">
<p>
    <span class="Title1">Navicat&trade;</span><br>
    <span class="Title2">The gateway to your database! (Python Version)</span>
</p>
<fieldset>
    <legend>System Environment Test</legend>
    <table>
        {system_tests}
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
</html>'''


def application(environ, start_response):
    """WSGI application entry point"""
    tunnel = NavicatTunnel()
    
    # Handle different request methods
    if environ['REQUEST_METHOD'] == 'POST':
        # Parse POST data
        try:
            request_body_size = int(environ.get('CONTENT_LENGTH', 0))
        except (ValueError):
            request_body_size = 0
        
        request_body = environ['wsgi.input'].read(request_body_size)
        params = parse_qs(request_body.decode('utf-8'))
        
        # Check if required parameters exist
        if not all(key in params for key in ['actn', 'host', 'port', 'login']):
            if not ALLOW_TEST_MENU:
                response_body = tunnel.echo_header(202) + tunnel.get_block("invalid parameters")
                start_response('200 OK', [
                    ('Content-Type', 'text/plain; charset=x-user-defined'),
                    ('Content-Length', str(len(response_body)))
                ])
                return [response_body]
            else:
                # Show test page
                response_body = tunnel.get_test_page_html().encode('utf-8')
                start_response('200 OK', [
                    ('Content-Type', 'text/html; charset=UTF-8'),
                    ('Content-Length', str(len(response_body)))
                ])
                return [response_body]
        
        # Handle actions
        action = params.get('actn', [''])[0]
        
        if action == 'C':
            # Connection test
            response_body = tunnel.handle_connection_test(params)
        elif action == 'Q':
            # Query execution
            response_body = tunnel.handle_query_execution(params)
        else:
            response_body = tunnel.echo_header(202) + tunnel.get_block("invalid action")
        
        start_response('200 OK', [
            ('Content-Type', 'text/plain; charset=x-user-defined'),
            ('Content-Length', str(len(response_body)))
        ])
        return [response_body]
    
    else:
        # GET request - show test page if allowed
        if ALLOW_TEST_MENU:
            response_body = tunnel.get_test_page_html().encode('utf-8')
            start_response('200 OK', [
                ('Content-Type', 'text/html; charset=UTF-8'),
                ('Content-Length', str(len(response_body)))
            ])
            return [response_body]
        else:
            start_response('403 Forbidden', [])
            return [b'Access denied']


if __name__ == '__main__':
    # Simple development server
    port = int(os.environ.get('PORT', 8000))
    print(f"Starting Navicat HTTP Tunnel on port {port}")
    print(f"MySQL support: {'Available' if MYSQL_AVAILABLE else 'Not available (install pymysql)'}")
    print(f"Access: http://localhost:{port}")
    
    httpd = make_server('', port, application)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\\nShutting down...")
        httpd.shutdown()