package navapp.graph

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// The whole graph: ten screens of BaseFlowActivity, whose inherited onCreate opens SessionActivity
// (a base: one row, not ten ways), and each opens HelpActivity through HelpNavigator (a navigator
// ten screens share: a hub, and a node every way of theirs passes).

object HelpNavigator {
    fun openHelp(from: Activity) = from.startActivity(Intent(from, HelpActivity::class.java))
}

abstract class BaseFlowActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (intent.getBooleanExtra("expired", false)) {
            startActivity(Intent(this, SessionActivity::class.java))
        }
    }
}

class HelpActivity : FragmentActivity()

class SessionActivity : FragmentActivity()

class FlowOneActivity : BaseFlowActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        findViewById<android.view.View>(android.R.id.content).setOnClickListener { HelpNavigator.openHelp(this) }
    }
}

class FlowTwoActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowThreeActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowFourActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowFiveActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowSixActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowSevenActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowEightActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowNineActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}

class FlowTenActivity : BaseFlowActivity() {
    override fun onResume() {
        super.onResume()
        HelpNavigator.openHelp(this)
    }
}
