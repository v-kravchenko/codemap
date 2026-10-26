package main

import "testing"

func TestIsDIGlue(t *testing.T) {
	for _, name := range []string{
		"a/B_Factory", "a/B_Factory$1", "a/B_FactoryX", "a/B_Factory_Impl", "a/B_MembersInjector", "a/X_HiltModules$Binds",
		"a/X_HiltModules_KeyModule", "a/M_ProvideApiFactory", "a/M_ProvideFactory", "a/M_ProvideApiFactoryX",
		"a/M_Provide_Factory$x", "a/Q_BindsX", "a/Q_Binds", "a/Q_Binds$Y", "a/Q_Binds/x", "DaggerApp", "a/DaggerApp$SubcomponentImpl", "a/NotDagger", "dagger/x",
		"hilt_aggregated_deps/_a", "x/hilt_aggregated_deps/y", "a/Plain", "a/Has_Under", "a/Has_Under$Factory",
		"a/V_Factory_Impl", "a/V_HiltModules_KeyModule_Provide_LazyMapKey", "a/V_Factory_Impl2",
		"a/A_B_Factory", "a/A_HiltComponents$SingletonC", "a/X_ComponentTreeDeps", "a/A_GeneratedInjector",
		"a/x_Provide$Factory", "a/_Factory", "_Binds", "a/b$_Factory", "a/B_Factor",
	} {
		if got, want := isDIGlue(name), diGlueRe.MatchString(name); got != want {
			t.Errorf("isDIGlue(%q) = %v, regexp %v", name, got, want)
		}
	}
}
