package volume

/*
	volume.Manager providers interfaces for both provisioning volume backends, and then creating volumes using them.

	There is one volume.Manager per host daemon process (though of course it's not an enforced singleton, because tests behave otherwise).
*/
type Manager struct {
}
