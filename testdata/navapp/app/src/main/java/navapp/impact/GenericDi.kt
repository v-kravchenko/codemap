package navapp.impact

import android.os.Bundle
import android.view.View
import androidx.fragment.app.Fragment
import androidx.lifecycle.ViewModel

// A ViewModel reaches the screens through a generic base: a change to CartViewModel affects
// CartFragment, not OrdersFragment, though both get their ViewModel from BaseVmFragment.

abstract class BaseVmFragment<VM : ViewModel> : Fragment() {
    lateinit var viewModel: VM

    abstract fun createViewModel(): VM

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        viewModel = createViewModel()
    }
}

class CartViewModel : ViewModel() {
    fun total(): Int = 1
}

class OrdersViewModel : ViewModel() {
    fun count(): Int = 2
}

class CartFragment : BaseVmFragment<CartViewModel>() {
    override fun createViewModel() = CartViewModel()

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        view.tag = viewModel.total()
    }
}

class OrdersFragment : BaseVmFragment<OrdersViewModel>() {
    override fun createViewModel() = OrdersViewModel()

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        view.tag = viewModel.count()
    }
}
