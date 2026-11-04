package navapp.watch

import androidx.fragment.app.FragmentActivity

class WatchedOneActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        StateWatcher.observe(this)
    }
}

class WatchedTwoActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        StateWatcher.observe(this)
    }
}

class FormOneActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormTwoActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormThreeActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormFourActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormFiveActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormSixActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormSevenActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormEightActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormNineActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}

class FormTenActivity : FragmentActivity() {
    override fun onResume() {
        super.onResume()
        FormBinder.bind(this)
    }
}
