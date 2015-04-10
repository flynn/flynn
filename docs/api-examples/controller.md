<h1>Controller</h1>


All requests are authenticated using [basic auth](https://en.wikipedia.org/wiki/Basic_access_authentication) where the username is blank and the password is your controller key. You may alternatively use the `key` URL parameter with the controller key for event streams (this allows using them via JavaScript in the browser). You can retrieve your controller key by running `flynn -a controller env get AUTH_KEY`.

The API root URL is the controller domain, by default `https://controller.$CLUSTER_DOMAIN`.
