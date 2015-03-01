Description
-----------

Event based irc client library.


Features
--------
* Event based. Register Callbacks for the events you need to handle.
* Handles basic irc demands for you
	* Standard CTCP
	* Reconnections on errors
	* Detect stoned servers

Install
-------
	$ go get github.com/thoj/go-ircevent

Example
-------
See test/irc_test.go

Events for callbacks
--------------------
* 001 Welcome
* PING
* CTCP Unknown CTCP
* CTCP_VERSION Version request (Handled internaly)
* CTCP_USERINFO
* CTCP_CLIENTINFO
* CTCP_TIME
* CTCP_PING
* CTCP_ACTION (/me)
* PRIVMSG
* MODE
* JOIN

+Many more


AddCallback Example
-------------------
	ircobj.AddCallback("PRIVMSG", func(event *irc.Event) {
		//e.Message() contains the message
		//e.Nick Contains the sender
		//e.Arguments[0] Contains the channel
	});

Commands
--------
	ircobj := irc.IRC("<nick>", "<user>") //Create new ircobj
	//Set options
	ircobj.UseTLS = true //default is false
	//ircobj.TLSOptions //set ssl options
	ircobj.Password = "[server password]"
	//Commands
	ircobj.Connect("irc.someserver.com:6667") //Connect to server
	ircobj.SendRaw("<string>") //sends string to server. Adds \r\n
	ircobj.SendRawf("<formatstring>", ...) //sends formatted string to server.n
	ircobj.Join("<#channel> [password]") 
	ircobj.Nick("newnick") 
	ircobj.Privmsg("<nickname | #channel>", "msg")
	ircobj.Privmsgf(<nickname | #channel>, "<formatstring>", ...)
	ircobj.Notice("<nickname | #channel>", "msg")
	ircobj.Noticef("<nickname | #channel>", "<formatstring>", ...)
