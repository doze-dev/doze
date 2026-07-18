package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/doze-dev/doze/internal/state"
	"github.com/doze-dev/doze/internal/ui"
)

// renderPlan prints a structural plan: changes grouped by instance, colorized
// (+ create / ~ update / - destroy), with a Terraform-style summary footer.
func renderPlan(plan state.Plan) {
	if plan.Empty() {
		fmt.Println("no changes — declared structure is up to date")
		return
	}
	fmt.Println("doze will perform the following actions:")
	fmt.Println()
	for _, ip := range plan.Instances {
		head := ip.Name
		if ip.Engine != "" {
			head = ip.Engine + "." + ip.Name
		}
		fmt.Println("  " + ui.Title(head))
		for _, c := range ip.Changes {
			fmt.Printf("    %s %-9s %s\n", changeSymbol(c.Kind), c.Object.Kind, c.Object.Name)
		}
		fmt.Println()
	}
	add, change, destroy := plan.Counts()
	fmt.Printf("plan: %d to add, %d to change, %d to destroy\n", add, change, destroy)
}

func changeSymbol(k state.ChangeKind) string {
	switch k {
	case state.Create:
		return ui.OK("+")
	case state.Delete:
		return ui.Fail("-")
	default:
		return ui.Warn("~")
	}
}

// confirm asks a yes/no question on stdin, defaulting to no.
func confirm(question string) bool {
	fmt.Printf("\n%s [y/N]: ", question)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// filterPlan narrows a plan to a single instance.
func filterPlan(plan state.Plan, name string) state.Plan {
	var out state.Plan
	for _, ip := range plan.Instances {
		if ip.Name == name {
			out.Instances = append(out.Instances, ip)
		}
	}
	return out
}
