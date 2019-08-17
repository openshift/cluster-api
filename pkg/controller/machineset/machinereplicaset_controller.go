/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package machineset

import (
	"context"
	"fmt"
	machinev1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	"github.com/openshift/cluster-api/pkg/util"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// controllerNameMRS is the name of this controller
	controllerNameMRS = "MRS_controller"
	// machineRoleLabel      = "machine.openshift.io/cluster-api-machine-role"
	// machineTypeLable      = "machine.openshift.io/cluster-api-machine-type"
	// masterMachineRoleType = "master"
	mrsFinalizer = "machine.openshift.io/mrs-managed"
)

// ReconcileMachineSet reconciles a MachineSet object
type ReconcileMachineReplicaSet struct {
	client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// newReconciler returns a new reconcile.Reconciler.
func newMRSReconciler(mgr manager.Manager) *ReconcileMachineReplicaSet {
	return &ReconcileMachineReplicaSet{Client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: mgr.GetEventRecorderFor(controllerName)}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func addMRS(mgr manager.Manager, r reconcile.Reconciler, mapFn handler.ToRequestsFunc) error {
	// Create a new controller.
	c, err := controller.New(controllerNameMRS, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Map Machine changes to MachineSets by machining labels.
	err = c.Watch(
		&source.Kind{Type: &machinev1beta1.Machine{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: mapFn},
	)
	if err != nil {
		return err
	}

	// Watch for changes to MachineSet.
	return c.Watch(
		&source.Kind{Type: &machinev1beta1.MachineReplicaSet{}},
		&handler.EnqueueRequestForObject{},
	)
}

func (r *ReconcileMachineReplicaSet) MachineToMRS(o handler.MapObject) []reconcile.Request {
	result := []reconcile.Request{}
	m := &machinev1beta1.Machine{}
	key := client.ObjectKey{Namespace: o.Meta.GetNamespace(), Name: o.Meta.GetName()}
	err := r.Client.Get(context.Background(), key, m)
	if err != nil {
		klog.Errorf("Unable to retrieve Machine %v from store: %v", key, err)
		return nil
	}
	/*
		ml := []machinev1beta1.Machine{
			*m,
		}
		matchingMachines := r.filterMRSMachines(ml)
		if len(matchingMachines) > 0 {
			name := client.ObjectKey{Namespace: m.Namespace, Name: "default"}
			result = append(result, reconcile.Request{NamespacedName: name})
		}
	*/
	// append everything for now
	machineReplicaSets := r.getMachineReplicaSetsForMachine(m)

	// If we have more than one match, something is wrong.
	if len(machineReplicaSets) == 1 {
		name := client.ObjectKey{Namespace: m.Namespace, Name: machineReplicaSets[0].Name}
		result = append(result, reconcile.Request{NamespacedName: name})
	}
	return result
}

func (r *ReconcileMachineReplicaSet) addMRSFinalizers(machines []machinev1beta1.Machine) error {
	for _, m := range machines {
		if !m.ObjectMeta.DeletionTimestamp.IsZero() {
			// Don't re-add finalizers to deleted machines
			continue
		}
		if !util.Contains(m.Finalizers, mrsFinalizer) {
			m.Finalizers = append(m.ObjectMeta.Finalizers, mrsFinalizer)
			ctx := context.TODO()
			if err := r.Client.Update(ctx, &m); err != nil {
				klog.Infof("Failed to add finalizers to machine %q: %v", m.Name, err)
				return err
			}
		}
	}
	return nil
}

// Reconcile reads that state of the cluster for a MachineReplicaSet object and makes changes based on the state read
// and what is in the MahcineReplicaSet.Spec
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=machine.openshift.io,resources=machinereplicasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=machine.openshift.io,resources=machines,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileMachineReplicaSet) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling: %v", request.NamespacedName)
	// Fetch the MachineSet instance
	ctx := context.TODO()
	if request.NamespacedName.Namespace != "openshift-machine-api" || request.NamespacedName.Name != "default" {
		klog.Error("We don't process those")
		return reconcile.Result{}, nil
	}
	mrs := &machinev1beta1.MachineReplicaSet{}
	if err := r.Get(ctx, request.NamespacedName, mrs); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	newMRS := mrs.DeepCopy()
	allMachinesList := &machinev1beta1.MachineList{}
	if err := r.Client.List(context.Background(), allMachinesList, client.InNamespace(mrs.Namespace)); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to list machines")
	}
	matchingMachines, err := r.filterMRSMachines(mrs, allMachinesList.Items)
	if err != nil {
		return reconcile.Result{}, err
	}

	// We only queue requests here if a machines is deleted, so don't return
	// after updating machines finalizers
	if err := r.addMRSFinalizers(matchingMachines); err != nil {
		return reconcile.Result{}, err
	}

	added := r.addMachineToStatus(&newMRS.Status.Replicas, matchingMachines)
	if added {
		// return since we updated, we'll reconcile again.
		return r.updateMRSStatus(newMRS)
	}
	klog.Infof("after add Machines")
	minReplicas := 3
	if mrs.Spec.MinReplicas != nil {
		minReplicas = int(*mrs.Spec.MinReplicas)
	}
	if len(mrs.Status.Replicas) < minReplicas {
		klog.Errorf("Less than 3 machines")
		// Less than 3 machines, don't do anything else.
		return reconcile.Result{}, nil
	}

	// We have at least 3 machines, now we need to do sanity checking and exit
	// early if things don't look right.

	inProcess := 0
	var replicaInProcess machinev1beta1.MachineReplica
	machineMissing := false
	var missingMachineName string
	klog.Infof("ranging mrs.Status.Replicas for validity")
	var mrIndex int
	var oldMachine machinev1beta1.Machine
	mrsToPop := []int{}
	for i, mr := range newMRS.Status.Replicas {
		if mr.ReplacementInProgress {
			inProcess += 1
			// Take a refernce so we can mutate it without doing crazy list splicing.
			replicaInProcess = mr
			mrIndex = i
		}
		machineFound := false
		for _, m := range matchingMachines {
			if m.Name == mr.Name {
				if replicaInProcess.Name == m.Name {
					// we have something in process, get the machine so we can
					// copy it.
					oldMachine = m
				}
				machineFound = true
				break
			}
		}
		if !machineFound {
			if len(mr.Replaces) > 0 {
				// If the machine is gone and it has a replacement listed,
				// We will remove it from Replicas
				mrsToPop = append(mrsToPop, i)
				continue
			}
			machineMissing = true
			missingMachineName = mr.Name
			break
		}
	}
	if machineMissing {
		// This means a machine we knew about previously has disappeared.
		// There is no way for us to replace it, and something went seriously
		// wrong.
		klog.Errorf("Unable to proceed.  A previously discovered machine is missing: %s", missingMachineName)
		// Should set some status and emit an event that this is fatal.
		return reconcile.Result{}, nil
	}
	// Process mrsToPop; this removes machines that are missing that have been replaced.
	if len(mrsToPop) > 0 {
		return r.updateMRL(newMRS, mrsToPop)
	}
	if inProcess > 1 {
		klog.Errorf("Unable to proceed.  More than one machine is marked as being replaced")
		return reconcile.Result{}, fmt.Errorf("Cannot process more than one replacement")
	} else if inProcess == 1 {
		klog.Infof("We're replacing something")
		if len(replicaInProcess.Replaces) == 0 {
			return r.addReplacedBy(newMRS, replicaInProcess, mrIndex)
		}
		return r.processReplace(newMRS, replicaInProcess, oldMachine)
	}

	// Nothing was found to be in process, let's look at the machines and
	// determine if we need to replace one.
	return r.processMachinesToReplace(newMRS, matchingMachines)
}

func (r *ReconcileMachineReplicaSet) updateMRL(newMRS *machinev1beta1.MachineReplicaSet, mrsToPop []int) (reconcile.Result, error) {
	newMRL := []machinev1beta1.MachineReplica{}
	for i, mr := range newMRS.Status.Replicas {
		skip := false
		for _, index := range mrsToPop {
			if i == index {
				// In the list to pop, let's skip it.
				skip = true
				break
			}
		}
		if skip {
			// Don't want to append it to the list.
			continue
		}
		newMRL = append(newMRL, mr)
	}
	// We had at least one item to remove (really should only ever be one max...)
	newMRS.Status.Replicas = newMRL
	klog.Infof("updating mrs to remove old machines: %v", newMRS.Status.Replicas)
	return r.updateMRSStatus(newMRS)
}

func (r *ReconcileMachineReplicaSet) updateMRSStatus(newMRS *machinev1beta1.MachineReplicaSet) (reconcile.Result, error) {
	klog.Infof("updating mrs status: %v", newMRS.Status)
	if err := r.Client.Status().Update(context.Background(), newMRS); err != nil {
		klog.Errorf("Failed to add machines: %v", err)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileMachineReplicaSet) processMachinesToReplace(newMRS *machinev1beta1.MachineReplicaSet, machines []machinev1beta1.Machine) (reconcile.Result, error) {
	var machineToReplace string
	for _, m := range machines {
		// More than one machine can have a deletion timestamp, that's okay.
		// We'll just process them one at a time.
		if !m.ObjectMeta.DeletionTimestamp.IsZero() {
			klog.Infof("Found machine with deletion timestamp")
			machineToReplace = m.Name
			break
		}
	}
	if machineToReplace != "" {
		klog.Infof("about to update status with machine")
		// We found a machine with deletion timestamp, let's update our status.
		for i, mr := range newMRS.Status.Replicas {
			if mr.Name == machineToReplace {
				mr.ReplacementInProgress = true
				// We're going to update our status so we requeue to process replacement.
				newMRS.Status.Replicas[i] = mr
				return r.updateMRSStatus(newMRS)
			}
		}
	}
	// No machines need to be replaced, return nil.
	return reconcile.Result{}, nil
}

func (r *ReconcileMachineReplicaSet) addReplacedBy(newMRS *machinev1beta1.MachineReplicaSet, replicaInProcess machinev1beta1.MachineReplica, mrIndex int) (reconcile.Result, error) {
	// Need to make sure this new name we're construction doesn't already exist as a machine.
	// This part is tricky....
	// Might want to create new machine, give it an annotation of some kind,
	// then scrape the name.
	replicaInProcess.Replaces = fmt.Sprintf("%sa", replicaInProcess.Name)
	newMRS.Status.Replicas[mrIndex] = replicaInProcess
	// Fill out the field with a new name and reconcile again next time
	klog.Infof("updating mrs status: %v", newMRS.Status)
	return r.updateMRSStatus(newMRS)
}

func (r *ReconcileMachineReplicaSet) processReplace(newMRS *machinev1beta1.MachineReplicaSet, replicaInProcess machinev1beta1.MachineReplica, oldMachine machinev1beta1.Machine) (reconcile.Result, error) {
	// This function does the real work.
	klog.Infof("processing replace of %v", replicaInProcess)

	newm := &machinev1beta1.Machine{}
	key := client.ObjectKey{Namespace: newMRS.ObjectMeta.GetNamespace(), Name: replicaInProcess.Replaces}
	err := r.Client.Get(context.Background(), key, newm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Replacement machine not found, we need to create it.
			// Don't return here because creating a machine doesn't requeue us
			err = r.createReplacementMachine(newMRS, replicaInProcess, oldMachine)
			if err != nil {
				return reconcile.Result{}, err
			}
			klog.Infof("Create machine success, returning err to requeue")
			return reconcile.Result{}, fmt.Errorf("returning err to requeue")
		}
		klog.Errorf("Unable to retrieve Machine %v from store: %v", key, err)
		return reconcile.Result{}, err
	}

	// So, the new machine now exists, wait for it to have nodeRef
	if newm.Status.NodeRef == nil || newm.Status.NodeRef.Name == "" {
		klog.Infof("Waiting for new machine to join the cluster, returning error to requeue")
		// Since we're watching for updates on machines, we probably don't need
		// to return error here, we'll be queued when nodelink adds the noderef
		// to the machine.
		return reconcile.Result{}, fmt.Errorf("returning err to requeue")
	}

	// Do etcd / DNS steps here, whatever those are.

	// New machine has nodeRef, and other steps are complete, we can remove finalizers
	// from the old machine
	oldMachine.ObjectMeta.Finalizers = util.Filter(oldMachine.ObjectMeta.Finalizers, mrsFinalizer)
	if err := r.Client.Update(context.Background(), &oldMachine); err != nil {
		klog.Errorf("Failed to remove finalizer from machine %q: %v", oldMachine.Name, err)
		return reconcile.Result{}, err
	}
	// And that should be it.
	return reconcile.Result{}, nil
}

func (r *ReconcileMachineReplicaSet) createReplacementMachine(newMRS *machinev1beta1.MachineReplicaSet, replicaInProcess machinev1beta1.MachineReplica, oldMachine machinev1beta1.Machine) error {
	klog.Infof("Replacement Creation Called")
	// createMachine creates a machine resource.
	// the name of the newly created resource is going to be created by the API server, we set the generateName field

	gv := machinev1beta1.SchemeGroupVersion
	om := metav1.ObjectMeta{
		Name:        replicaInProcess.Replaces,
		Labels:      oldMachine.Labels,
		Annotations: oldMachine.Annotations,
	}
	machine := &machinev1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       gv.WithKind("Machine").Kind,
			APIVersion: gv.String(),
		},
		ObjectMeta: om,
		Spec:       oldMachine.Spec,
	}
	machine.Namespace = oldMachine.Namespace

	if err := r.Client.Create(context.Background(), machine); err != nil {
		klog.Errorf("Unable to create Machine %q: %v", machine.Name, err)
		return err
	}

	return nil
}

func (r *ReconcileMachineReplicaSet) addMachineToStatus(mrSet *[]machinev1beta1.MachineReplica, machines []machinev1beta1.Machine) bool {
	added := false
	for _, machine := range machines {
		exists := false
		for _, mr := range *mrSet {
			if machine.Name == mr.Name {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		klog.Infof("adding machine to status: %v", machine.Name)
		mrToAdd := machinev1beta1.MachineReplica{
			Name:                  machine.Name,
			ReplacementInProgress: false,
		}
		*mrSet = append(*mrSet, mrToAdd)
		added = true
	}
	return added
}

func (r *ReconcileMachineReplicaSet) filterMRSMachines(mrs *machinev1beta1.MachineReplicaSet, machines []machinev1beta1.Machine) ([]machinev1beta1.Machine, error) {
	matchingMachines := []machinev1beta1.Machine{}
	selector, err := metav1.LabelSelectorAsSelector(&mrs.Spec.Selector)
	if err != nil {
		klog.Errorf("unable to convert selector: %v", err)
		return matchingMachines, err
	}
	for _, machine := range machines {
		if hasMatchingMRSLabels(selector, &machine) {
			matchingMachines = append(matchingMachines, machine)
		}
	}
	return matchingMachines, nil
}

func hasMatchingMRSLabels(selector labels.Selector, machine *machinev1beta1.Machine) bool {
	if selector.Empty() {
		return false
	}

	if !selector.Matches(labels.Set(machine.Labels)) {
		return false
	}
	return true
}

func (c *ReconcileMachineReplicaSet) getMachineReplicaSetsForMachine(m *machinev1beta1.Machine) []*machinev1beta1.MachineReplicaSet {
	if len(m.Labels) == 0 {
		klog.Warningf("No machine sets found for Machine %v because it has no labels", m.Name)
		return nil
	}

	mrsList := &machinev1beta1.MachineReplicaSetList{}
	err := c.Client.List(context.Background(), mrsList, client.InNamespace(m.Namespace))
	if err != nil {
		klog.Errorf("Failed to list machine sets, %v", err)
		return nil
	}

	var machineReplicaSets []*machinev1beta1.MachineReplicaSet
	for idx := range mrsList.Items {
		mrs := &mrsList.Items[idx]
		selector, err := metav1.LabelSelectorAsSelector(&mrs.Spec.Selector)
		if err != nil {
			// Ignore MRS with broken selectors
			continue
		}
		if hasMatchingMRSLabels(selector, m) {
			machineReplicaSets = append(machineReplicaSets, mrs)
		}
	}

	return machineReplicaSets
}
