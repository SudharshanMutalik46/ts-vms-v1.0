using System;
using System.Globalization;
using System.Windows;
using System.Windows.Data;

namespace TSVmsDesktop.Converters
{
    /// <summary>
    /// Converts a boolean to Visibility: true → Collapsed, false → Visible.
    /// Used to hide elements when a condition is true (e.g., hide grid when full screen is active).
    /// </summary>
    public class InverseBooleanToVisibilityConverter : IValueConverter
    {
        public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        {
            if (value is bool b && b)
                return Visibility.Collapsed;
            return Visibility.Visible;
        }

        public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        {
            return value is Visibility v && v != Visibility.Visible;
        }
    }
}
