<?php	//version my202 - PHP 8 compatible

//set allowTestMenu to false to disable System/Server test page
$allowTestMenu = true;

// Force mysqli usage since mysql_* functions are removed in PHP 7.0+
$use_mysqli = true;

header("Content-Type: text/plain; charset=x-user-defined");
error_reporting(E_ALL);
set_time_limit(0);

function phpversion_int()
{
	list($maVer, $miVer, $edVer) = preg_split("(/|\.|-)", phpversion());
	return $maVer*10000 + $miVer*100 + $edVer;
}

// Remove magic quotes handling as it's removed in PHP 8
// Magic quotes were removed in PHP 5.4.0

function GetLongBinary($num)
{
	return pack("N",$num);
}

function GetShortBinary($num)
{
	return pack("n",$num);
}

function GetDummy($count)
{
	$str = "";
	for($i=0;$i<$count;$i++)
		$str .= "\x00";
	return $str;
}

function GetBlock($val)
{
	$len = strlen($val);
	if( $len < 254 )
		return chr($len).$val;
	else
		return "\xFE".GetLongBinary($len).$val;
}

function EchoHeader($errno)
{
	$str = GetLongBinary(1111);
	$str .= GetShortBinary(202);
	$str .= GetLongBinary($errno);
	$str .= GetDummy(6);
	echo $str;
}

function EchoConnInfo($conn)
{
	$str = GetBlock(mysqli_get_host_info($conn));
	$str .= GetBlock(mysqli_get_proto_info($conn));
	$str .= GetBlock(mysqli_get_server_info($conn));
	echo $str;
}

function EchoResultSetHeader($errno, $affectrows, $insertid, $numfields, $numrows)
{
	$str = GetLongBinary($errno);
	$str .= GetLongBinary($affectrows);
	$str .= GetLongBinary($insertid);
	$str .= GetLongBinary($numfields);
	$str .= GetLongBinary($numrows);
	$str .= GetDummy(12);
	echo $str;
}

function EchoFieldsHeader($res, $numfields)
{
	$str = "";
	for( $i = 0; $i < $numfields; $i++ ) {
		$finfo = mysqli_fetch_field_direct($res, $i);
		$str .= GetBlock($finfo->name);
		$str .= GetBlock($finfo->table);
		
		$type = $finfo->type;
		$length = $finfo->length;
	
		$str .= GetLongBinary($type);

		$intflag = $finfo->flags;
		$str .= GetLongBinary($intflag);

		$str .= GetLongBinary($length);
	}
	echo $str;
}

function EchoData($res, $numfields, $numrows)
{
	for( $i = 0; $i < $numrows; $i++ ) {
		$str = "";
		$row = mysqli_fetch_row( $res );
		for( $j = 0; $j < $numfields; $j++ ){
			if( is_null($row[$j]) )
				$str .= "\xFF";
			else
				$str .= GetBlock($row[$j]);
		}
		echo $str;
	}
}

function doSystemTest()
{
	function output($description, $succ, $resStr) {
		echo "<tr><td class=\"TestDesc\">$description</td><td ";
		echo ($succ)? "class=\"TestSucc\">$resStr[0]</td></tr>" : "class=\"TestFail\">$resStr[1]</td></tr>";
	}
	output("PHP version >= 7.0", phpversion_int() >= 70000, array("Yes", "No"));
	output("mysqli_connect() available", function_exists("mysqli_connect"), array("Yes", "No"));
	
	// Check for mod_security2 if possible
	if (function_exists("apache_get_modules") && is_array(apache_get_modules())){
		if (in_array("mod_security2", apache_get_modules()))
			output("Mod Security 2 installed", false, array("No", "Yes"));
	}
}

/////////////////////////////////////////////////////////////////////////////
////

	if (phpversion_int() < 70000) {
		EchoHeader(201);
		echo GetBlock("unsupported php version - requires PHP 7.0+");
		exit();
	}

	// Handle old PHP versions POST variables - not needed for PHP 8 but keep for compatibility
	if (phpversion_int() < 40010) {
		global $HTTP_POST_VARS;
		$_POST = &$HTTP_POST_VARS;	
	}

	$testMenu = false;
	if (!isset($_POST["actn"]) || !isset($_POST["host"]) || !isset($_POST["port"]) || !isset($_POST["login"])) {
		$testMenu = $allowTestMenu;
		if (!$testMenu){
			EchoHeader(202);
			echo GetBlock("invalid parameters");
			exit();
		}
	}

	if (!$testMenu){
		if (isset($_POST["encodeBase64"]) && $_POST["encodeBase64"] == '1') {
			for($i=0;$i<count($_POST["q"]);$i++)
				$_POST["q"][$i] = base64_decode($_POST["q"][$i]);
		}
		
		if (!function_exists("mysqli_connect")) {
			EchoHeader(203);
			echo GetBlock("MySQL not supported on the server");
			exit();
		}
		
		$errno_c = 0;
		$hs = $_POST["host"];
		
		// Only use mysqli since mysql_* functions are removed
		if( $_POST["port"] && $_POST["port"] != '' ) 
			$conn = mysqli_connect($hs, $_POST["login"], $_POST["password"], '', (int)$_POST["port"]);
		else
			$conn = mysqli_connect($hs, $_POST["login"], $_POST["password"]);
			
		if (!$conn) {
			$errno_c = mysqli_connect_errno();
			EchoHeader($errno_c);
			echo GetBlock(mysqli_connect_error());
			exit();
		}
		
		// Set charset for unicode support
		mysqli_set_charset($conn, 'utf8');
		
		if(isset($_POST["db"]) && $_POST["db"] != "" ) {
			$res = mysqli_select_db($conn, $_POST["db"] );
			$errno_c = mysqli_errno($conn);
		}

		EchoHeader($errno_c);
		if($errno_c > 0) {
			echo GetBlock(mysqli_error($conn));
		} elseif($_POST["actn"] == "C") {
			EchoConnInfo($conn);
		} elseif($_POST["actn"] == "Q") {
			for($i=0;$i<count($_POST["q"]);$i++) {
				$query = $_POST["q"][$i];
				if($query == "") continue;
				
				// Magic quotes handling removed - not needed in PHP 8
				
				$res = mysqli_query($conn, $query);
				$errno = mysqli_errno($conn);
				$affectedrows = mysqli_affected_rows($conn);
				$insertid = mysqli_insert_id($conn);				
				
				if (false !== $res) {
					$numfields = mysqli_field_count($conn);
					// Only call mysqli_num_rows if we have a result object (SELECT queries)
					if (is_object($res)) {
						$numrows = mysqli_num_rows($res);
					} else {
						$numrows = 0;
					}
				} else {
					$numfields = 0;
					$numrows = 0;
				}
				
				EchoResultSetHeader($errno, $affectedrows, $insertid, $numfields, $numrows);
				
				if($errno > 0) {
					echo GetBlock(mysqli_error($conn));
				} else {
					if($numfields > 0 && is_object($res)) {
						EchoFieldsHeader($res, $numfields);
						EchoData($res, $numfields, $numrows);
					} else {
						$info = mysqli_info($conn);
						echo GetBlock($info ? $info : "");
					}
				}
				
				if($i<(count($_POST["q"])-1))
					echo "\x01";
				else
					echo "\x00";
					
				if (is_object($res))
					mysqli_free_result($res);
			}
		}
		
		mysqli_close($conn);
		exit();
	}

	header("Content-Type: text/html");
////
/////////////////////////////////////////////////////////////////////////////
?>

<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
<html>
<head>
	<title>Navicat HTTP Tunnel Tester (PHP 8 Compatible)</title>
	<meta http-equiv="Content-Type" content="text/html; charset=UTF-8">
	<style type="text/css">
		body{
			margin: 30px;
			font-family: Tahoma;
			font-weight: normal;
			font-size: 14px;
			color: #222222;
		}
		table{
			width: 100%;
			border: 0px;
		}
		input{
			font-family:Tahoma,sans-serif;
			border-style:solid;
			border-color:#666666;
			border-width:1px;
		}
		fieldset{
			border-style:solid;
			border-color:#666666;
			border-width:1px;
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
			width:70%
		}
		.TestSucc{
			color: #00BB00;
		}
		.TestFail{
			color: #DD0000;
		}
		.mysql{
		}
		.pgsql{
			display:none;
		}
		.sqlite{
			display:none;
		}
		#page{
			max-width: 42em;
			min-width: 36em;
			border-width: 0px;
			margin: auto auto;
		}
		#host, #dbfile{
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
	function getInternetExplorerVersion(){
		var ver = -1;
		if (navigator.appName == "Microsoft Internet Explorer"){
			var regex = new RegExp("MSIE ([0-9]{1,}[\.0-9]{0,})");
			if (regex.exec(navigator.userAgent))
				ver = parseFloat(RegExp.$1);
		}
		return ver;
	}
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
		var version = getInternetExplorerVersion();
		if (version==-1 || version>=9.0){
			var xmlhttp = (window.XMLHttpRequest)? new XMLHttpRequest() : new ActiveXObject("Microsoft.XMLHTTP");
			
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
			xmlhttp.setRequestHeader("Content-length", params.length);
			xmlhttp.setRequestHeader("Connection", "close");
			xmlhttp.send(params);
		}else{
			document.getElementById("ServerTest").className = "";
			document.getElementById("ServerTest").innerHTML = "Internet Explorer "+version+" is not supported, please use Internet explorer 9.0 or above, firefox, chrome or safari";
		}
	}
	</script>
</head>

<body>
<div id="page">
<p>
	<font class="Title1">Navicat&trade;</font><br>
	<font class="Title2">The gateway to your database! (PHP 8 Compatible)</font>
</p>
<fieldset>
	<legend>System Environment Test</legend>
	<table>
		<tr style="<?php echo "display:none"; ?>"><td width=70%>PHP installed properly</td><td class="TestFail">No</td></tr>
		<?php doSystemTest();?>
	</table>
</fieldset>
<br>
<fieldset>
	<legend>Server Test</legend>
	<form id="TestServerForm" action="#" onSubmit="return false;">
	<input type=hidden id="actn" value="C">
	<table>
		<tr class="mysql"><td width="35%">Hostname/IP Address:</td><td><input type=text id="host" placeholder="localhost"></td></tr>
		<tr class="mysql"><td>Port:</td><td><input type=text id="port" placeholder="3306"></td></tr>
		<tr class="pgsql"><td>Initial Database:</td><td><input type=text id="db" placeholder="template1"></td></tr>
		<tr class="mysql"><td>Username:</td><td><input type=text id="login" placeholder="root"></td></tr>
		<tr class="mysql"><td>Password:</td><td><input type=password id="password" placeholder=""></td></tr>
		<tr class="sqlite"><td>Database File:</td><td><input type=text id="dbfile" placeholder="sqlite.db"></td></tr>
		<tr><td></td><td><br><input id="TestButton" type="submit" value="Test Connection" onClick="doServerTest()"></td></tr>
	</table>
	</form>
	<div id="ServerTest"><br></div>
</fieldset>
<p id="Copyright">Copyright &copy; PremiumSoft &trade; CyberTech Ltd. All Rights Reserved.</p>
</div>
</body>
</html>